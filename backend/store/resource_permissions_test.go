package store

import (
	"context"
	"testing"
)

func TestResourcePermissionsReturnsSubjectsAndActions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddFolder(ctx, "p1", "/Shared")
	_ = tx.AddPipeline(ctx, "p1", 12, "Deploy", "/Shared", "enabled")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddSubject(ctx, "g-1", "Devs", "vsts", "group")
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 1, "ViewBuilds", "View builds")
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 2, "QueueBuilds", "Queue builds")
	// Alice has direct ViewBuilds+QueueBuilds on the pipeline
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1/Shared/12", "u-1", 3, 0, 0, 0, 3, 0)
	// Devs group has direct ViewBuilds on the folder
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1/Shared", "g-1", 1, 0, 0, 0, 1, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	detail, err := s.ResourcePermissionsByRun(ctx, runID, "p1/Shared/12")
	if err != nil {
		t.Fatalf("ResourcePermissionsByRun: %v", err)
	}
	if detail.Resource.Token != "p1/Shared/12" || detail.Resource.Type != "pipeline" {
		t.Fatalf("unexpected resource: %+v", detail.Resource)
	}
	if len(detail.Subjects) != 1 {
		t.Fatalf("expected 1 subject on pipeline, got %d: %+v", len(detail.Subjects), detail.Subjects)
	}
	if detail.Subjects[0].Subject.Descriptor != "u-1" {
		t.Fatalf("expected u-1, got %+v", detail.Subjects[0].Subject)
	}
	// Alice has 2 actions at this token
	if len(detail.Subjects[0].Permissions) != 2 {
		t.Fatalf("expected 2 permissions, got %d", len(detail.Subjects[0].Permissions))
	}
	// Check folder permissions
	folderDetail, err := s.ResourcePermissionsByRun(ctx, runID, "p1/Shared")
	if err != nil {
		t.Fatalf("ResourcePermissionsByRun folder: %v", err)
	}
	if len(folderDetail.Subjects) != 1 || folderDetail.Subjects[0].Subject.Descriptor != "g-1" {
		t.Fatalf("expected g-1 on folder, got %+v", folderDetail.Subjects)
	}
}

func TestResourcePermissionsNotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 1, "ViewBuilds", "View builds")
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuild, "p1", "u-1", 1, 0, 0, 0, 1, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	_, err := s.ResourcePermissionsByRun(ctx, runID, "p1/missing")
	if err != ErrNotFound {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestResourcePermissionsRejectsUnknownToken(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	runID, _ := s.BeginRun(ctx, "acme")

	_, err := s.ResourcePermissionsByRun(ctx, runID, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown run/resource")
	}
}