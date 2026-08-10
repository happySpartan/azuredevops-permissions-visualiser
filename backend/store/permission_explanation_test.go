package store

import (
	"context"
	"testing"
)

func TestPermissionExplanationTracesMembershipAndResourceInheritance(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddFolder(ctx, "p1", "/Shared")
	_ = tx.AddPipeline(ctx, "p1", 12, "Deploy", "/Shared", "enabled")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddSubject(ctx, "g-team", "Delivery Team", "vsts", "group")
	_ = tx.AddSubject(ctx, "g-admin", "Build Admins", "vsts", "group")
	_ = tx.AddMembership(ctx, "g-team", "u-1")
	_ = tx.AddMembership(ctx, "g-admin", "g-team")
	_ = tx.AddPermissionAction(ctx, 1, "ViewBuilds", "View builds")
	_ = tx.AddAssignmentExtended(ctx, "p1", "g-admin", 1, 0, 0, 0, 1, 0)
	_ = tx.AddAssignmentExtended(ctx, "p1/Shared", "g-team", 0, 1, 0, 0, 0, 1)
	_ = tx.AddAssignmentExtended(ctx, "p1/Shared/12", "u-1", 1, 0, 0, 0, 1, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	explanation, err := s.PermissionExplanationByRun(ctx, runID, "u-1", "p1/Shared/12", 1)
	if err != nil {
		t.Fatalf("PermissionExplanationByRun: %v", err)
	}
	if explanation.Permission.Name != "ViewBuilds" || explanation.State != PermissionAllow {
		t.Fatalf("unexpected verdict: %+v", explanation)
	}
	if len(explanation.ResourcePath) != 3 || explanation.ResourcePath[0].Token != "p1" || explanation.ResourcePath[2].Token != "p1/Shared/12" {
		t.Fatalf("unexpected resource path: %+v", explanation.ResourcePath)
	}
	if len(explanation.Evidence) != 3 {
		t.Fatalf("evidence count = %d, want 3: %+v", len(explanation.Evidence), explanation.Evidence)
	}
	admin := explanation.Evidence[0]
	if admin.Subject.Descriptor != "g-admin" || admin.State != PermissionAllow || len(admin.MembershipPath) != 3 {
		t.Fatalf("unexpected admin evidence: %+v", admin)
	}
	if admin.MembershipPath[0].Descriptor != "u-1" || admin.MembershipPath[2].Descriptor != "g-admin" {
		t.Fatalf("unexpected membership path: %+v", admin.MembershipPath)
	}
	if !admin.FromAncestor || !admin.ViaGroup {
		t.Fatalf("expected inherited group evidence: %+v", admin)
	}
}

func TestPermissionExplanationHandlesMembershipCycles(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddSubject(ctx, "g-1", "Group One", "vsts", "group")
	_ = tx.AddSubject(ctx, "g-2", "Group Two", "vsts", "group")
	_ = tx.AddMembership(ctx, "g-1", "u-1")
	_ = tx.AddMembership(ctx, "g-2", "g-1")
	_ = tx.AddMembership(ctx, "g-1", "g-2")
	_ = tx.AddPermissionAction(ctx, 1, "ViewBuilds", "View builds")
	_ = tx.AddAssignmentExtended(ctx, "p1", "u-1", 0, 0, 0, 0, 1, 0)
	_ = tx.AddAssignmentExtended(ctx, "p1", "g-2", 1, 0, 0, 0, 1, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	explanation, err := s.PermissionExplanationByRun(ctx, runID, "u-1", "p1", 1)
	if err != nil {
		t.Fatalf("PermissionExplanationByRun: %v", err)
	}
	if len(explanation.Evidence) != 1 || len(explanation.Evidence[0].MembershipPath) != 3 {
		t.Fatalf("unexpected cyclic explanation: %+v", explanation)
	}
}

func TestPermissionExplanationRejectsUnknownAction(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	runID, _ := s.BeginRun(ctx, "acme")

	_, err := s.PermissionExplanationByRun(ctx, runID, "missing", "p1", 99)
	if err != ErrNotFound {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
