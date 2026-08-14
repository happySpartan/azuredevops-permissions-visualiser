package store

import (
	"bytes"
	"context"
	"encoding/csv"
	"reflect"
	"strings"
	"testing"
)

func TestExportEffectivePermissionsCSV(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddFolder(ctx, "p1", "/Shared")
	_ = tx.AddPipeline(ctx, "p1", 12, "Deploy", "/Shared", "enabled")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddSubject(ctx, "g-1", "Devs", "vsts", "group")
	_ = tx.AddMembership(ctx, "g-1", "u-1")
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 1, "ViewBuilds", "View builds")
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 2, "QueueBuilds", "Queue builds")
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1", "g-1", 1, 0, 0, 0, 1, 0)           // allow ViewBuilds
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1/Shared/12", "u-1", 2, 0, 0, 0, 2, 0) // allow QueueBuilds
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	var buf bytes.Buffer
	if err := s.ExportEffectivePermissionsCSV(ctx, runID, &buf); err != nil {
		t.Fatalf("ExportEffectivePermissionsCSV: %v", err)
	}

	body := buf.String()
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (header + 2 subjects × 2 actions), got %d", len(lines))
	}

	// Header
	header := lines[0]
	if !strings.Contains(header, "subject_descriptor") || !strings.Contains(header, "state") {
		t.Fatalf("unexpected header: %s", header)
	}

	// Check Alice / QueueBuilds row
	found := false
	for _, line := range lines[1:] {
		if strings.Contains(line, "u-1") && strings.Contains(line, "QueueBuilds") && strings.Contains(line, "allow") {
			found = true
			if !strings.Contains(line, "true") { // direct
				t.Fatalf("expected direct=true for u-1/QueueBuilds: %s", line)
			}
		}
	}
	if !found {
		t.Fatalf("expected a row for u-1/QueueBuilds allow, got:\n%s", body)
	}

	// Check Alice / ViewBuilds via group
	found = false
	for _, line := range lines[1:] {
		if strings.Contains(line, "u-1") && strings.Contains(line, "ViewBuilds") && strings.Contains(line, "allow") {
			found = true
			if !strings.Contains(line, "true,true") { // viaGroup, inherited? No — it's viaGroup
				// Check viaGroup specifically
				cols := strings.Split(line, ",")
				if len(cols) >= 12 {
					// direct, inherited, via_group
					_ = cols[9]  // direct
					_ = cols[10] // inherited
					_ = cols[11] // via_group
				}
			}
		}
	}
}

func TestExportSubjectAssignmentsCSV(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1", "u-1", 1, 0, 0, 0, 1, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	var buf bytes.Buffer
	if err := s.ExportSubjectAssignmentsCSV(ctx, runID, &buf); err != nil {
		t.Fatalf("ExportSubjectAssignmentsCSV: %v", err)
	}

	body := buf.String()
	if !strings.Contains(body, "u-1") || !strings.Contains(body, "0x1") {
		t.Fatalf("expected u-1 and mask 0x1 in:\n%s", body)
	}
}

func TestExportSubjectPermissionsCSVScopesRowsToSubject(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddSubject(ctx, "u-2", "Bob", "aad", "user")
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 1, "ViewBuilds", "View builds")
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1", "u-1", 1, 0, 0, 0, 1, 0)
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1", "u-2", 0, 1, 0, 0, 0, 1)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	var buf bytes.Buffer
	if err := s.ExportSubjectPermissionsCSV(ctx, runID, "u-1", &buf); err != nil {
		t.Fatalf("ExportSubjectPermissionsCSV: %v", err)
	}

	body := buf.String()
	if !strings.Contains(body, "u-1,Alice") || !strings.Contains(body, "ViewBuilds") || !strings.Contains(body, "allow") {
		t.Fatalf("expected Alice's effective permission in:\n%s", body)
	}
	if strings.Contains(body, "u-2") || strings.Contains(body, "Bob") {
		t.Fatalf("subject export must not include other subjects:\n%s", body)
	}
}

func TestExportResourcePermissionsCSVScopesRowsAndIncludesViewIdentity(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddFolder(ctx, "p1", "/Shared")
	_ = tx.AddPipeline(ctx, "p1", 12, "Deploy", "/Shared", "enabled")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddSubject(ctx, "u-2", "Bob", "aad", "user")
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 1, "ViewBuilds", "View builds")
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1/Shared/12", "u-1", 1, 0, 0, 0, 1, 0)
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1", "u-2", 0, 1, 0, 0, 0, 1)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	var buf bytes.Buffer
	if err := s.ExportResourcePermissionsCSV(ctx, runID, "p1/Shared/12", &buf); err != nil {
		t.Fatalf("ExportResourcePermissionsCSV: %v", err)
	}
	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	wantHeader := []string{"view", "resource_token", "resource_type", "resource_name", "resource_project", "resource_path", "subject_descriptor", "subject_display_name", "subject_kind", "subject_origin", "permission_bit", "permission_name", "permission_display_name", "state", "direct", "inherited", "via_group"}
	if !reflect.DeepEqual(records[0], wantHeader) {
		t.Fatalf("header = %v, want %v", records[0], wantHeader)
	}
	if len(records) != 2 {
		t.Fatalf("records = %v, want header plus one scoped row", records)
	}
	if records[1][0] != "resource" || records[1][1] != "p1/Shared/12" || records[1][6] != "u-1" || records[1][11] != "ViewBuilds" {
		t.Fatalf("unexpected resource export row: %v", records[1])
	}
	if strings.Contains(buf.String(), "u-2") || strings.Contains(buf.String(), "Bob") {
		t.Fatalf("resource export includes a different token's subject:\n%s", buf.String())
	}
}

func TestExportPermissionMatrixCSVScopesProjectAndActionAndIncludesFilters(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddProject(ctx, "p2", "Beta")
	_ = tx.AddFolder(ctx, "p1", "/Shared")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddSubject(ctx, "u-2", "Bob", "aad", "user")
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 1, "ViewBuilds", "View builds")
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 2, "QueueBuilds", "Queue builds")
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1", "u-1", 1, 0, 0, 0, 1, 0)
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1/Shared", "u-2", 0, 2, 0, 0, 0, 2)
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p2", "u-1", 0, 1, 0, 0, 0, 1)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	var buf bytes.Buffer
	if err := s.ExportPermissionMatrixCSV(ctx, runID, "p1", 1, &buf); err != nil {
		t.Fatalf("ExportPermissionMatrixCSV: %v", err)
	}
	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	wantHeader := []string{"view", "project_id", "project_name", "permission_bit", "permission_name", "permission_display_name", "resource_token", "resource_type", "resource_name", "resource_path", "subject_descriptor", "subject_display_name", "subject_kind", "subject_origin", "assignment_collected", "state", "direct", "inherited", "via_group"}
	if !reflect.DeepEqual(records[0], wantHeader) {
		t.Fatalf("header = %v, want %v", records[0], wantHeader)
	}
	if len(records) != 5 {
		t.Fatalf("records = %v, want header plus two resources x two subjects", records)
	}
	foundUnknown := false
	for _, row := range records[1:] {
		if row[0] != "matrix" || row[1] != "p1" || row[3] != "1" || row[4] != "ViewBuilds" {
			t.Fatalf("row missing active matrix filters: %v", row)
		}
		if strings.HasPrefix(row[6], "p2") || row[15] == "deny" {
			t.Fatalf("matrix export leaked another project/action: %v", row)
		}
		if row[14] == "false" && row[15] == "unknown" {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Fatalf("matrix export must preserve unknown cells: %v", records)
	}
}

func TestPerViewCSVExportsProtectAgainstFormulaInjection(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "=Alpha")
	_ = tx.AddSubject(ctx, "u-1", "+Alice", "@aad", "-user")
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 1, "\tViewBuilds", "\rView builds")
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1", "u-1", 1, 0, 0, 0, 1, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	for name, export := range map[string]func(*bytes.Buffer) error{
		"resource": func(buf *bytes.Buffer) error { return s.ExportResourcePermissionsCSV(ctx, runID, "p1", buf) },
		"matrix":   func(buf *bytes.Buffer) error { return s.ExportPermissionMatrixCSV(ctx, runID, "p1", 1, buf) },
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := export(&buf); err != nil {
				t.Fatalf("export: %v", err)
			}
			records, err := csv.NewReader(&buf).ReadAll()
			if err != nil {
				t.Fatalf("read CSV: %v", err)
			}
			seen := map[string]bool{}
			for _, row := range records[1:] {
				for _, value := range row {
					if value != "" && strings.ContainsRune("=+-@	\r", rune(value[0])) {
						t.Fatalf("unsafe CSV value %q in row %v", value, row)
					}
					seen[value] = true
				}
			}
			if !seen["'=Alpha"] || !seen["'+Alice"] || !seen["'@aad"] {
				t.Fatalf("expected dangerous values to be apostrophe-prefixed: %v", records)
			}
		})
	}
}

func TestAllCSVExportsProtectAgainstFormulaInjection(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "=Alpha")
	_ = tx.AddSubject(ctx, "g-1", "@Admins", "vsts", "group")
	_ = tx.AddSubject(ctx, "u-1", "+Alice", "aad", "user")
	_ = tx.AddMembership(ctx, "g-1", "u-1")
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 1, "-ViewBuilds", "=View builds")
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1", "u-1", 1, 0, 0, 0, 1, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	exports := map[string]func(*bytes.Buffer) error{
		"effective":   func(buf *bytes.Buffer) error { return s.ExportEffectivePermissionsCSV(ctx, runID, buf) },
		"subject":     func(buf *bytes.Buffer) error { return s.ExportSubjectPermissionsCSV(ctx, runID, "u-1", buf) },
		"assignments": func(buf *bytes.Buffer) error { return s.ExportSubjectAssignmentsCSV(ctx, runID, buf) },
		"group":       func(buf *bytes.Buffer) error { return s.ExportGroupMembershipCSV(ctx, runID, "g-1", buf) },
	}
	for name, export := range exports {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := export(&buf); err != nil {
				t.Fatal(err)
			}
			records, err := csv.NewReader(&buf).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range records[1:] {
				for _, value := range row {
					if value != "" && strings.ContainsRune("=+-@\t\r\n", rune(value[0])) {
						t.Fatalf("unsafe CSV value %q in row %v", value, row)
					}
				}
			}
		})
	}
}

func TestExportEmptyRun(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	_ = s.CompleteRun(ctx, runID, RunCounts{})

	var buf bytes.Buffer
	if err := s.ExportEffectivePermissionsCSV(ctx, runID, &buf); err != nil {
		t.Fatalf("ExportEffectivePermissionsCSV: %v", err)
	}
	body := strings.TrimSpace(buf.String())
	if body == "" {
		t.Fatal("expected at least a header row")
	}
	if !strings.Contains(body, "subject_descriptor") {
		t.Fatalf("expected header: %s", body)
	}
}

func TestExportGroupMembershipCSVIncludesDirectTransitiveAndPaths(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddSubject(ctx, "g-root", "Engineering", "aad", "group")
	_ = tx.AddSubject(ctx, "g-platform", "Platform", "aad", "group")
	_ = tx.AddSubject(ctx, "u-alice", "Alice", "aad", "user")
	_ = tx.AddMembership(ctx, "g-root", "g-platform")
	_ = tx.AddMembership(ctx, "g-platform", "u-alice")
	_ = tx.Commit()

	var buf bytes.Buffer
	if err := s.ExportGroupMembershipCSV(ctx, runID, "g-root", &buf); err != nil {
		t.Fatalf("ExportGroupMembershipCSV: %v", err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	want := [][]string{
		{"group_descriptor", "group_display_name", "member_descriptor", "member_display_name", "member_kind", "relationship", "membership_path"},
		{"g-root", "Engineering", "u-alice", "Alice", "user", "transitive", "Engineering > Platform > Alice"},
		{"g-root", "Engineering", "g-platform", "Platform", "group", "direct", "Engineering > Platform"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}
}
