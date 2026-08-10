package store

import (
	"context"
	"testing"
)

func TestSubjectPermissionsByRunReturnsAzureDevOpsEffectiveResults(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddFolder(ctx, "p1", "/Shared")
	_ = tx.AddPipeline(ctx, "p1", 12, "Deploy", "/Shared", "enabled")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddPermissionAction(ctx, 1, "ViewBuilds", "View builds")
	_ = tx.AddPermissionAction(ctx, 2, "EditBuildDefinition", "Edit build pipeline")
	_ = tx.AddAssignmentExtended(ctx, "p1/Shared/12", "u-1", 0, 0, 1, 0, 3, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	detail, err := s.SubjectPermissionsByRun(ctx, runID, "u-1")
	if err != nil {
		t.Fatalf("SubjectPermissionsByRun: %v", err)
	}
	if detail.Subject.DisplayName != "Alice" || len(detail.Resources) != 1 {
		t.Fatalf("unexpected detail: %+v", detail)
	}
	resource := detail.Resources[0]
	if resource.Type != "pipeline" || resource.Name != "Deploy" || len(resource.Permissions) != 2 {
		t.Fatalf("unexpected resource: %+v", resource)
	}
	view := resource.Permissions[0]
	if view.Name != "ViewBuilds" || view.State != PermissionAllow || !view.Inherited || view.Direct || view.ViaGroup {
		t.Fatalf("unexpected inherited permission: %+v", view)
	}
	edit := resource.Permissions[1]
	if edit.State != PermissionAllow || edit.Direct || edit.Inherited || !edit.ViaGroup {
		t.Fatalf("unexpected group-derived permission: %+v", edit)
	}
}

func TestSubjectPermissionsByRunIncludesExplicitDeny(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddSubject(ctx, "g-1", "Readers", "vsts", "group")
	_ = tx.AddPermissionAction(ctx, 1, "ViewBuilds", "View builds")
	_ = tx.AddAssignmentExtended(ctx, "p1", "g-1", 0, 1, 0, 0, 0, 1)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	detail, err := s.SubjectPermissionsByRun(ctx, runID, "g-1")
	if err != nil {
		t.Fatalf("SubjectPermissionsByRun: %v", err)
	}
	permission := detail.Resources[0].Permissions[0]
	if permission.State != PermissionDeny || !permission.Direct || permission.Inherited || permission.ViaGroup {
		t.Fatalf("unexpected deny: %+v", permission)
	}
}

func TestSubjectPermissionsByRunRejectsUnknownSubject(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	runID, _ := s.BeginRun(ctx, "acme")

	_, err := s.SubjectPermissionsByRun(ctx, runID, "missing")
	if err != ErrNotFound {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
