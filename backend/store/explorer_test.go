package store

import (
	"context"
	"testing"
)

func TestResourcesByRunBuildsProjectHierarchy(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p2", "Beta")
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddFolder(ctx, "p1", "/Shared")
	_ = tx.AddPipeline(ctx, "p1", 12, "Deploy", "/Shared", "enabled")
	_ = tx.AddPipeline(ctx, "p1", 7, "Build", "/", "enabled")
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	resources, err := s.ResourcesByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ResourcesByRun: %v", err)
	}
	if len(resources) != 2 || resources[0].Name != "Alpha" || resources[1].Name != "Beta" {
		t.Fatalf("unexpected projects: %+v", resources)
	}
	alpha := resources[0]
	if len(alpha.Folders) != 1 || alpha.Folders[0].Path != "/Shared" {
		t.Fatalf("unexpected folders: %+v", alpha.Folders)
	}
	if len(alpha.Pipelines) != 2 || alpha.Pipelines[0].Name != "Build" || alpha.Pipelines[1].Name != "Deploy" {
		t.Fatalf("unexpected pipelines: %+v", alpha.Pipelines)
	}
}

func TestSubjectsByRunFiltersAndPaginates(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddSubject(ctx, "u-1", "Alice Adams", "aad", "user")
	_ = tx.AddSubject(ctx, "g-1", "Build Administrators", "vsts", "group")
	_ = tx.AddSubject(ctx, "u-2", "Bob Brown", "aad", "user")
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	page, err := s.SubjectsByRun(ctx, runID, SubjectQuery{Search: "b", Limit: 1})
	if err != nil {
		t.Fatalf("SubjectsByRun: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].DisplayName != "Bob Brown" {
		t.Fatalf("unexpected first page: %+v", page)
	}

	page, err = s.SubjectsByRun(ctx, runID, SubjectQuery{Search: "b", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("SubjectsByRun second page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].DisplayName != "Build Administrators" {
		t.Fatalf("unexpected second page: %+v", page)
	}
}

func TestSubjectsByRunCapsPageSize(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	page, err := s.SubjectsByRun(ctx, runID, SubjectQuery{Limit: 5000})
	if err != nil {
		t.Fatalf("SubjectsByRun: %v", err)
	}
	if page.Limit != 200 {
		t.Fatalf("limit = %d, want 200", page.Limit)
	}
}
