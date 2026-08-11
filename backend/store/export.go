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
		"project", "resource_token",
		"permission_name", "permission_display_name",
		"state", "direct", "inherited", "via_group",
	}
	if err := cw.Write(header); err != nil {
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
			if err := cw.Write(row); err != nil {
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

	if err := cw.Write([]string{
		"descriptor", "display_name", "kind", "origin",
		"security_token", "allow_mask", "deny_mask",
		"effective_allow_mask", "effective_deny_mask",
	}); err != nil {
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

		if err := cw.Write([]string{
			descriptor, displayName.String, kind.String, origin.String,
			token,
			fmt.Sprintf("0x%X", uint64(allow)),
			fmt.Sprintf("0x%X", uint64(deny)),
			fmt.Sprintf("0x%X", uint64(effAllow)),
			fmt.Sprintf("0x%X", uint64(effDeny)),
		}); err != nil {
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
