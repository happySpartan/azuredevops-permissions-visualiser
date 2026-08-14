package store

import (
	"context"
	"testing"
)

func TestPermissionMatrixScopesOneActionToOneProject(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddProject(ctx, "p2", "Beta")
	_ = tx.AddFolder(ctx, "p1", "/Shared")
	_ = tx.AddPipeline(ctx, "p1", 12, "Deploy", "/Shared", "enabled")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddSubject(ctx, "g-1", "Devs", "vsts", "group")
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 1, "ViewBuilds", "View builds")
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 2, "QueueBuilds", "Queue builds")
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1/Shared", "g-1", 0, 0, 1, 0, 1, 0)
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1/Shared/12", "u-1", 2, 0, 0, 0, 2, 0)
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1/Shared/12", "g-1", 0, 0, 0, 1, 0, 1)
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p2", "u-1", 1, 0, 0, 0, 1, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	matrix, err := s.PermissionMatrixByRun(ctx, runID, "p1", 1)
	if err != nil {
		t.Fatalf("PermissionMatrixByRun: %v", err)
	}
	if matrix.Action.Name != "ViewBuilds" {
		t.Fatalf("action = %+v, want ViewBuilds", matrix.Action)
	}
	if len(matrix.Subjects) != 2 {
		t.Fatalf("subjects = %+v, want Alice and Devs", matrix.Subjects)
	}
	if len(matrix.Rows) != 3 {
		t.Fatalf("rows = %+v, want three p1 resources", matrix.Rows)
	}
	if matrix.Rows[0].Resource.Token != "p1" {
		t.Fatalf("first resource = %+v, want p1", matrix.Rows[0].Resource)
	}
	if cell := matrix.Rows[1].Cells["g-1"]; cell == nil || cell.State != PermissionAllow || !cell.Inherited {
		t.Fatalf("folder Devs cell = %+v, want inherited allow", cell)
	}
	if matrix.Rows[2].Resource.Token != "p1/Shared/12" {
		t.Fatalf("third resource = %+v, want p1/Shared/12", matrix.Rows[2].Resource)
	}
	if cell := matrix.Rows[2].Cells["g-1"]; cell == nil || cell.State != PermissionDeny || !cell.Inherited {
		t.Fatalf("pipeline Devs cell = %+v, want inherited deny", cell)
	}
	if cell := matrix.Rows[2].Cells["u-1"]; cell == nil || cell.State != PermissionNotSet {
		t.Fatalf("pipeline Alice cell = %+v, want collected not-set for ViewBuilds", cell)
	}
}

func TestPermissionMatrixRejectsUnknownAction(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	runID, _ := s.BeginRun(ctx, "acme")

	_, err := s.PermissionMatrixByRun(ctx, runID, "p1", 128)
	if err != ErrNotFound {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
