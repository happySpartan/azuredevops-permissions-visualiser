package store

import (
	"context"
	"testing"
)

func TestSubjectPermissionsByRunIncludesGitRepositoriesAndBranches(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddRepository(ctx, "p1", "REPO-1", "mainrepo", "refs/heads/main")
	_ = tx.AddBranch(ctx, "REPO-1", "refs/heads/main")
	_ = tx.AddBranch(ctx, "REPO-1", "refs/heads/develop")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddPermissionAction(ctx, NamespaceGit, 2, "GenericRead", "Read")
	_ = tx.AddPermissionAction(ctx, NamespaceGit, 4, "GenericContribute", "Contribute")
	// Alice has direct Read+Contribute on the repository.
	_ = tx.AddAssignmentExtended(ctx, NamespaceGit, "REPO-1", "u-1", 6, 0, 0, 0, 6, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	detail, err := s.SubjectPermissionsByRun(ctx, runID, "u-1")
	if err != nil {
		t.Fatalf("SubjectPermissionsByRun: %v", err)
	}
	if len(detail.Resources) != 1 {
		t.Fatalf("resources = %+v, want the repository", detail.Resources)
	}
	resource := detail.Resources[0]
	if resource.Namespace != NamespaceGit || resource.Type != "repository" || resource.Name != "mainrepo" || resource.ProjectName != "Alpha" {
		t.Fatalf("unexpected resource: %+v", resource)
	}
	if len(resource.Permissions) != 2 {
		t.Fatalf("permissions = %+v, want Git Read and Contribute", resource.Permissions)
	}
	read := resource.Permissions[0]
	if read.Name != "GenericRead" || read.State != PermissionAllow || !read.Direct {
		t.Fatalf("unexpected read permission: %+v", read)
	}
}

func TestResourcePermissionsByRunResolvesGitBranchToken(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddRepository(ctx, "p1", "REPO-1", "mainrepo", "refs/heads/main")
	_ = tx.AddBranch(ctx, "REPO-1", "refs/heads/main")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddPermissionAction(ctx, NamespaceGit, 2, "GenericRead", "Read")
	_ = tx.AddAssignmentExtended(ctx, NamespaceGit, "REPO-1/refs/heads/main", "u-1", 2, 0, 0, 0, 2, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	detail, err := s.ResourcePermissionsByRun(ctx, runID, "REPO-1/refs/heads/main")
	if err != nil {
		t.Fatalf("ResourcePermissionsByRun: %v", err)
	}
	if detail.Resource.Namespace != NamespaceGit || detail.Resource.Type != "branch" || detail.Resource.Name != "main" {
		t.Fatalf("unexpected resource: %+v", detail.Resource)
	}
	if len(detail.Subjects) != 1 || detail.Subjects[0].Subject.Descriptor != "u-1" {
		t.Fatalf("subjects = %+v, want u-1", detail.Subjects)
	}
	permission := detail.Subjects[0].Permissions[0]
	if permission.Name != "GenericRead" || permission.State != PermissionAllow {
		t.Fatalf("unexpected permission: %+v", permission)
	}
}

func TestPermissionExplanationByRunUsesNamespaceActions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddRepository(ctx, "p1", "REPO-1", "mainrepo", "refs/heads/main")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	// Both namespaces define bit 2: Build has EditBuildQuality, Git has GenericRead.
	_ = tx.AddPermissionAction(ctx, NamespaceBuild, 2, "EditBuildQuality", "Edit build quality")
	_ = tx.AddPermissionAction(ctx, NamespaceGit, 2, "GenericRead", "Read")
	_ = tx.AddAssignmentExtended(ctx, NamespaceGit, "REPO-1", "u-1", 2, 0, 0, 0, 2, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	explanation, err := s.PermissionExplanationByRun(ctx, runID, "u-1", "REPO-1", 2)
	if err != nil {
		t.Fatalf("PermissionExplanationByRun: %v", err)
	}
	if explanation.Permission.Name != "GenericRead" {
		t.Fatalf("permission = %+v, want the Git namespace GenericRead action", explanation.Permission)
	}
	if explanation.Permission.State != PermissionAllow || !explanation.Permission.Direct {
		t.Fatalf("unexpected permission state: %+v", explanation.Permission)
	}
}
