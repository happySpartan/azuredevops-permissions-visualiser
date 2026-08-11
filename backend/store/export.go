package store

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ExportEffectivePermissionsCSV writes a denormalised CSV of every
// subject × resource × permission result to w. Each row shows the
// effective state plus provenance flags from the collected run.
func (s *Store) ExportEffectivePermissionsCSV(ctx context.Context, runID int64, w io.Writer) error {
	return s.exportEffectivePermissionsCSV(ctx, runID, "", w)
}

// ExportSubjectPermissionsCSV writes the effective-permission rows shown in
// the subject explorer for one subject.
func (s *Store) ExportSubjectPermissionsCSV(ctx context.Context, runID int64, descriptor string, w io.Writer) error {
	if descriptor == "" {
		return errors.New("store: subject descriptor is required")
	}
	return s.exportEffectivePermissionsCSV(ctx, runID, descriptor, w)
}

// ExportResourcePermissionsCSV writes exactly the rows displayed for one
// secured resource, including the active resource identity on every row.
func (s *Store) ExportResourcePermissionsCSV(ctx context.Context, runID int64, token string, w io.Writer) error {
	if token == "" {
		return errors.New("store: resource token is required")
	}
	detail, err := s.ResourcePermissionsByRun(ctx, runID, token)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(safeCSVRecord([]string{
		"view", "resource_token", "resource_type", "resource_name", "resource_project", "resource_path",
		"subject_descriptor", "subject_display_name", "subject_kind", "subject_origin",
		"permission_bit", "permission_name", "permission_display_name", "state", "direct", "inherited", "via_group",
	})); err != nil {
		return fmt.Errorf("store: resource export header: %w", err)
	}
	for _, entry := range detail.Subjects {
		for _, permission := range entry.Permissions {
			if err := cw.Write(safeCSVRecord([]string{
				"resource", detail.Resource.Token, detail.Resource.Type, detail.Resource.Name, detail.Resource.ProjectName, detail.Resource.Path,
				entry.Subject.Descriptor, entry.Subject.DisplayName, entry.Subject.Kind, entry.Subject.Origin,
				fmt.Sprint(permission.Bit), permission.Name, permission.DisplayName, string(permission.State),
				boolStr(permission.Direct), boolStr(permission.Inherited), boolStr(permission.ViaGroup),
			})); err != nil {
				return fmt.Errorf("store: resource export row: %w", err)
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

// ExportPermissionMatrixCSV writes the currently selected project/action
// matrix, including unknown cells where no assignment was collected.
func (s *Store) ExportPermissionMatrixCSV(ctx context.Context, runID int64, projectID string, bit int64, w io.Writer) error {
	if projectID == "" || bit <= 0 {
		return errors.New("store: matrix project and positive permission bit are required")
	}
	matrix, err := s.PermissionMatrixByRun(ctx, runID, projectID, bit)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(safeCSVRecord([]string{
		"view", "project_id", "project_name", "permission_bit", "permission_name", "permission_display_name",
		"resource_token", "resource_type", "resource_name", "resource_path",
		"subject_descriptor", "subject_display_name", "subject_kind", "subject_origin",
		"assignment_collected", "state", "direct", "inherited", "via_group",
	})); err != nil {
		return fmt.Errorf("store: matrix export header: %w", err)
	}
	for _, row := range matrix.Rows {
		for _, subject := range matrix.Subjects {
			permission := row.Cells[subject.Descriptor]
			collected, state, direct, inherited, viaGroup := false, "unknown", false, false, false
			if permission != nil {
				collected, state = true, string(permission.State)
				direct, inherited, viaGroup = permission.Direct, permission.Inherited, permission.ViaGroup
			}
			if err := cw.Write(safeCSVRecord([]string{
				"matrix", matrix.ProjectID, matrix.ProjectName, fmt.Sprint(matrix.Action.Bit), matrix.Action.Name, matrix.Action.DisplayName,
				row.Resource.Token, row.Resource.Type, row.Resource.Name, row.Resource.Path,
				subject.Descriptor, subject.DisplayName, subject.Kind, subject.Origin,
				boolStr(collected), state, boolStr(direct), boolStr(inherited), boolStr(viaGroup),
			})); err != nil {
				return fmt.Errorf("store: matrix export row: %w", err)
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

// ExportGroupMembershipCSV writes the selected group's direct and transitive
// members, including the deterministic path that establishes each membership.
func (s *Store) ExportGroupMembershipCSV(ctx context.Context, runID int64, descriptor string, w io.Writer) error {
	if descriptor == "" {
		return errors.New("store: group descriptor is required")
	}
	detail, err := s.GroupMembershipByRun(ctx, runID, descriptor)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(safeCSVRecord([]string{
		"group_descriptor", "group_display_name", "member_descriptor",
		"member_display_name", "member_kind", "relationship", "membership_path",
	})); err != nil {
		return fmt.Errorf("store: export group membership header: %w", err)
	}
	for _, member := range detail.Members {
		relationship := "transitive"
		if member.Direct {
			relationship = "direct"
		}
		path := make([]string, len(member.Path))
		for index, subject := range member.Path {
			path[index] = subject.DisplayName
		}
		if err := cw.Write(safeCSVRecord([]string{
			detail.Group.Descriptor, detail.Group.DisplayName,
			member.Subject.Descriptor, member.Subject.DisplayName, member.Subject.Kind,
			relationship, strings.Join(path, " > "),
		})); err != nil {
			return fmt.Errorf("store: export group membership row: %w", err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("store: export group membership: %w", err)
	}
	return nil
}

func (s *Store) exportEffectivePermissionsCSV(ctx context.Context, runID int64, descriptor string, w io.Writer) error {
	actions, err := permissionActionsByRun(ctx, s.db, runID)
	if err != nil {
		return fmt.Errorf("store: export actions: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT a.security_token, a.descriptor,
		       a.allow_bitmask, a.deny_bitmask,
		       a.inherited_allow_bitmask, a.inherited_deny_bitmask,
		       a.effective_allow_bitmask, a.effective_deny_bitmask,
		       s.display_name, s.subject_kind, s.origin,
		       p.name AS project_name
		FROM assignments a
		JOIN subjects s ON s.run_id = a.run_id AND s.descriptor = a.descriptor
		LEFT JOIN projects p ON p.run_id = a.run_id AND p.org_id = SUBSTR(a.security_token, 1, INSTR(a.security_token || '/', '/') - 1)
		WHERE a.run_id = ? AND (? = '' OR a.descriptor = ?)
		ORDER BY a.descriptor, a.security_token`, runID, descriptor, descriptor)
	if err != nil {
		return fmt.Errorf("store: export query: %w", err)
	}
	defer rows.Close()

	cw := csv.NewWriter(w)
	defer cw.Flush()

	// Header
	header := []string{
		"subject_descriptor", "subject_display_name", "subject_kind", "subject_origin",
		"project", "resource_token", "resource_type",
		"permission_name", "permission_display_name",
		"state", "direct", "inherited", "via_group",
	}
	if err := cw.Write(safeCSVRecord(header)); err != nil {
		return fmt.Errorf("store: export header: %w", err)
	}

	for rows.Next() {
		var token, descriptor string
		var allow, deny, inheritedAllow, inheritedDeny, effectiveAllow, effectiveDeny int64
		var displayName, kind, origin, projectName sql.NullString

		if err := rows.Scan(&token, &descriptor, &allow, &deny,
			&inheritedAllow, &inheritedDeny,
			&effectiveAllow, &effectiveDeny,
			&displayName, &kind, &origin,
			&projectName); err != nil {
			return fmt.Errorf("store: export scan: %w", err)
		}

		// Resolve resource type from token
		resType := resourceTypeFromToken(token)

		for _, action := range actions {
			state := stateForBit(effectiveAllow, effectiveDeny, action.Bit)
			direct := allow&action.Bit != 0 || deny&action.Bit != 0
			inherited := inheritedAllow&action.Bit != 0 || inheritedDeny&action.Bit != 0
			viaGroup := state != PermissionNotSet && !direct && !inherited

			row := []string{
				descriptor,
				displayName.String,
				kind.String,
				origin.String,
				projectName.String,
				token,
				resType,
				action.Name,
				action.DisplayName,
				string(state),
				boolStr(direct),
				boolStr(inherited),
				boolStr(viaGroup),
			}
			if err := cw.Write(safeCSVRecord(row)); err != nil {
				return fmt.Errorf("store: export row: %w", err)
			}
		}
	}
	return rows.Err()
}

// ExportSubjectAssignmentsCSV writes a subject-centric CSV with one row per
// raw assignment (ACE), showing the bitmask columns as hex.
func (s *Store) ExportSubjectAssignmentsCSV(ctx context.Context, runID int64, w io.Writer) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.security_token, a.descriptor,
		       a.allow_bitmask, a.deny_bitmask,
		       a.effective_allow_bitmask, a.effective_deny_bitmask,
		       s.display_name, s.subject_kind, s.origin
		FROM assignments a
		JOIN subjects s ON s.run_id = a.run_id AND s.descriptor = a.descriptor
		WHERE a.run_id = ?
		ORDER BY a.descriptor, a.security_token`, runID)
	if err != nil {
		return fmt.Errorf("store: export assignments: %w", err)
	}
	defer rows.Close()

	cw := csv.NewWriter(w)
	defer cw.Flush()

	if err := cw.Write(safeCSVRecord([]string{
		"descriptor", "display_name", "kind", "origin",
		"security_token", "allow_mask", "deny_mask",
		"effective_allow_mask", "effective_deny_mask",
	})); err != nil {
		return fmt.Errorf("store: export header: %w", err)
	}

	for rows.Next() {
		var token, descriptor string
		var allow, deny, effAllow, effDeny int64
		var displayName, kind, origin sql.NullString

		if err := rows.Scan(&token, &descriptor, &allow, &deny,
			&effAllow, &effDeny, &displayName, &kind, &origin); err != nil {
			return fmt.Errorf("store: export scan: %w", err)
		}

		if err := cw.Write(safeCSVRecord([]string{
			descriptor, displayName.String, kind.String, origin.String,
			token,
			fmt.Sprintf("0x%X", uint64(allow)),
			fmt.Sprintf("0x%X", uint64(deny)),
			fmt.Sprintf("0x%X", uint64(effAllow)),
			fmt.Sprintf("0x%X", uint64(effDeny)),
		})); err != nil {
			return fmt.Errorf("store: export row: %w", err)
		}
	}
	return rows.Err()
}

func resourceTypeFromToken(token string) string {
	parts := strings.Split(token, "/")
	switch len(parts) {
	case 1:
		return "project"
	case 2:
		return "folder"
	default:
		return "pipeline"
	}
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// safeCSVRecord prevents spreadsheet applications from interpreting exported
// values as formulas.
func safeCSVRecord(record []string) []string {
	safe := make([]string, len(record))
	for i, value := range record {
		if value != "" && strings.ContainsRune("=+-@	\r\n", rune(value[0])) {
			value = "'" + value
		}
		safe[i] = value
	}
	return safe
}
