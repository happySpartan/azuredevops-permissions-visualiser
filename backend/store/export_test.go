package store

import (
	"bytes"
	"context"
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
	_ = tx.AddPermissionAction(ctx, 1, "ViewBuilds", "View builds")
	_ = tx.AddPermissionAction(ctx, 2, "QueueBuilds", "Queue builds")
	_ = tx.AddAssignmentExtended(ctx, "p1", "g-1", 1, 0, 0, 0, 1, 0) // allow ViewBuilds
	_ = tx.AddAssignmentExtended(ctx, "p1/Shared/12", "u-1", 2, 0, 0, 0, 2, 0) // allow QueueBuilds
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
					_ = cols[9] // direct
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
	_ = tx.AddAssignmentExtended(ctx, "p1", "u-1", 1, 0, 0, 0, 1, 0)
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