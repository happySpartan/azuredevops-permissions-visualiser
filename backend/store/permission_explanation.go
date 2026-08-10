package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// PermissionExplanation traces the collected evidence for one effective result.
type PermissionExplanation struct {
	Subject      Subject              `json:"subject"`
	Resource     PermissionResource   `json:"resource"`
	Permission   PermissionResult     `json:"permission"`
	State        PermissionState      `json:"state"`
	ResourcePath []PermissionResource `json:"resourcePath"`
	Evidence     []PermissionEvidence `json:"evidence"`
}

// PermissionEvidence is one raw ACE that can contribute to the result.
type PermissionEvidence struct {
	Token          string             `json:"token"`
	Resource       PermissionResource `json:"resource"`
	Subject        Subject            `json:"subject"`
	State          PermissionState    `json:"state"`
	ExplicitAllow  bool               `json:"explicitAllow"`
	ExplicitDeny   bool               `json:"explicitDeny"`
	EffectiveAllow bool               `json:"effectiveAllow"`
	EffectiveDeny  bool               `json:"effectiveDeny"`
	FromAncestor   bool               `json:"fromAncestor"`
	ViaGroup       bool               `json:"viaGroup"`
	MembershipPath []Subject          `json:"membershipPath"`
}

// PermissionExplanationByRun returns raw ACE, membership, and resource ancestry
// evidence for one subject × secured resource × permission tuple. The verdict is
// always read from Azure DevOps' stored effective masks; evidence is explanatory
// and is not used to calculate a competing result.
func (s *Store) PermissionExplanationByRun(ctx context.Context, runID int64, descriptor, token string, bit int64) (*PermissionExplanation, error) {
	subject, err := s.subjectByDescriptor(ctx, runID, descriptor)
	if err != nil {
		return nil, err
	}
	action, err := s.permissionActionByBit(ctx, runID, bit)
	if err != nil {
		return nil, err
	}
	resource, err := s.permissionResource(ctx, runID, token)
	if err != nil {
		return nil, err
	}

	var allow, deny, inheritedAllow, inheritedDeny, effectiveAllow, effectiveDeny int64
	err = s.db.QueryRowContext(ctx, `
		SELECT allow_bitmask, deny_bitmask, inherited_allow_bitmask,
		       inherited_deny_bitmask, effective_allow_bitmask, effective_deny_bitmask
		FROM assignments WHERE run_id=? AND descriptor=? AND security_token=?`,
		runID, descriptor, token).
		Scan(&allow, &deny, &inheritedAllow, &inheritedDeny, &effectiveAllow, &effectiveDeny)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: explanation verdict: %w", err)
	}

	state := stateForBit(effectiveAllow, effectiveDeny, bit)
	permission := PermissionResult{
		Bit: bit, Name: action.Name, DisplayName: action.DisplayName, State: state,
		Direct:    allow&bit != 0 || deny&bit != 0,
		Inherited: inheritedAllow&bit != 0 || inheritedDeny&bit != 0,
		ViaGroup:  state != PermissionNotSet && allow&bit == 0 && deny&bit == 0 && inheritedAllow&bit == 0 && inheritedDeny&bit == 0,
	}

	resourcePath, err := s.resourcePath(ctx, runID, token)
	if err != nil {
		return nil, err
	}
	membershipPaths, err := s.membershipPaths(ctx, runID, subject)
	if err != nil {
		return nil, err
	}
	evidence, err := s.permissionEvidence(ctx, runID, token, bit, membershipPaths)
	if err != nil {
		return nil, err
	}

	return &PermissionExplanation{
		Subject: subject, Resource: resource, Permission: permission, State: state,
		ResourcePath: resourcePath, Evidence: evidence,
	}, nil
}

func (s *Store) subjectByDescriptor(ctx context.Context, runID int64, descriptor string) (Subject, error) {
	var subject Subject
	err := s.db.QueryRowContext(ctx, `
		SELECT descriptor, display_name, origin, subject_kind
		FROM subjects WHERE run_id=? AND descriptor=?`, runID, descriptor).
		Scan(&subject.Descriptor, &subject.DisplayName, &subject.Origin, &subject.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		return subject, ErrNotFound
	}
	return subject, err
}

func (s *Store) permissionActionByBit(ctx context.Context, runID, bit int64) (permissionAction, error) {
	var action permissionAction
	err := s.db.QueryRowContext(ctx, `
		SELECT bit, name, display_name FROM permission_actions WHERE run_id=? AND bit=?`, runID, bit).
		Scan(&action.Bit, &action.Name, &action.DisplayName)
	if errors.Is(err, sql.ErrNoRows) {
		return action, ErrNotFound
	}
	return action, err
}

func stateForBit(allow, deny, bit int64) PermissionState {
	if deny&bit != 0 {
		return PermissionDeny
	}
	if allow&bit != 0 {
		return PermissionAllow
	}
	return PermissionNotSet
}

func ancestorTokens(token string) []string {
	parts := strings.Split(token, "/")
	out := make([]string, 0, len(parts))
	for i := range parts {
		out = append(out, strings.Join(parts[:i+1], "/"))
	}
	return out
}

func (s *Store) resourcePath(ctx context.Context, runID int64, token string) ([]PermissionResource, error) {
	path := make([]PermissionResource, 0)
	for _, ancestor := range ancestorTokens(token) {
		resource, err := s.permissionResource(ctx, runID, ancestor)
		if err != nil {
			return nil, err
		}
		path = append(path, resource)
	}
	return path, nil
}

func (s *Store) membershipPaths(ctx context.Context, runID int64, subject Subject) (map[string][]Subject, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.parent_descriptor, m.member_descriptor,
		       p.display_name, p.origin, p.subject_kind
		FROM memberships m
		LEFT JOIN subjects p ON p.run_id=m.run_id AND p.descriptor=m.parent_descriptor
		WHERE m.run_id=? ORDER BY m.parent_descriptor, m.member_descriptor`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type parent struct{ subject Subject }
	parents := map[string][]parent{}
	for rows.Next() {
		var parentDescriptor, memberDescriptor string
		var name, origin, kind sql.NullString
		if err := rows.Scan(&parentDescriptor, &memberDescriptor, &name, &origin, &kind); err != nil {
			return nil, err
		}
		parents[memberDescriptor] = append(parents[memberDescriptor], parent{Subject{Descriptor: parentDescriptor, DisplayName: name.String, Origin: origin.String, Kind: kind.String}})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	paths := map[string][]Subject{subject.Descriptor: {subject}}
	queue := []string{subject.Descriptor}
	for len(queue) > 0 {
		member := queue[0]
		queue = queue[1:]
		for _, next := range parents[member] {
			if _, seen := paths[next.subject.Descriptor]; seen {
				continue
			}
			path := append([]Subject{}, paths[member]...)
			path = append(path, next.subject)
			paths[next.subject.Descriptor] = path
			queue = append(queue, next.subject.Descriptor)
		}
	}
	return paths, nil
}

func (s *Store) permissionEvidence(ctx context.Context, runID int64, targetToken string, bit int64, paths map[string][]Subject) ([]PermissionEvidence, error) {
	tokens := ancestorTokens(targetToken)
	tokenOrder := make(map[string]int, len(tokens))
	for index, token := range tokens {
		tokenOrder[token] = index
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT security_token, descriptor, allow_bitmask, deny_bitmask,
		       effective_allow_bitmask, effective_deny_bitmask
		FROM assignments WHERE run_id=? ORDER BY security_token, descriptor`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type rawEvidence struct {
		token, descriptor                          string
		allow, deny, effectiveAllow, effectiveDeny int64
	}
	var raw []rawEvidence
	for rows.Next() {
		var item rawEvidence
		if err := rows.Scan(&item.token, &item.descriptor, &item.allow, &item.deny, &item.effectiveAllow, &item.effectiveDeny); err != nil {
			return nil, err
		}
		if _, relevantToken := tokenOrder[item.token]; !relevantToken {
			continue
		}
		if _, relevantSubject := paths[item.descriptor]; !relevantSubject {
			continue
		}
		if item.allow&bit == 0 && item.deny&bit == 0 {
			continue
		}
		raw = append(raw, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	evidence := make([]PermissionEvidence, 0, len(raw))
	for _, item := range raw {
		assignedSubject := paths[item.descriptor][len(paths[item.descriptor])-1]
		resource, err := s.permissionResource(ctx, runID, item.token)
		if err != nil {
			return nil, err
		}
		evidence = append(evidence, PermissionEvidence{
			Token: item.token, Resource: resource, Subject: assignedSubject,
			State:         stateForBit(item.allow, item.deny, bit),
			ExplicitAllow: item.allow&bit != 0, ExplicitDeny: item.deny&bit != 0,
			EffectiveAllow: item.effectiveAllow&bit != 0, EffectiveDeny: item.effectiveDeny&bit != 0,
			FromAncestor: item.token != targetToken, ViaGroup: len(paths[item.descriptor]) > 1,
			MembershipPath: paths[item.descriptor],
		})
	}
	return evidence, nil
}
