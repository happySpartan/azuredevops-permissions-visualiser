package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenSecuresDataDirectoryAndDatabase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	path := filepath.Join(dir, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	dbInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("dir mode = %o, want 700", got)
	}
	if got := dbInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("db mode = %o, want 600", got)
	}
}

func TestBeginAndCompleteRun(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id, err := s.BeginRun(ctx, "acme")
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero run id")
	}

	run, err := s.RunByID(ctx, id)
	if err != nil {
		t.Fatalf("RunByID: %v", err)
	}
	if run.Status != StatusRunning || run.Org != "acme" {
		t.Fatalf("unexpected run: %+v", run)
	}

	err = s.CompleteRun(ctx, id, RunCounts{Projects: 3, Pipelines: 5})
	if err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}

	run, _ = s.RunByID(ctx, id)
	if run.Status != StatusComplete || run.ProjectCount != 3 || run.PipelineCount != 5 || run.CompletedAt == nil {
		t.Fatalf("unexpected completed run: %+v", run)
	}
}

func TestRetainLatestOnly(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id1, _ := s.BeginRun(ctx, "acme")
	s.CompleteRun(ctx, id1, RunCounts{Projects: 1})

	id2, _ := s.BeginRun(ctx, "acme")
	s.CompleteRun(ctx, id2, RunCounts{Projects: 2})

	latest, _ := s.LatestRunID(ctx)
	if latest != id2 {
		t.Fatalf("latest = %d, want %d", latest, id2)
	}

	// The first run must have been purged (retain latest only).
	if _, err := s.RunByID(ctx, id1); err == nil {
		t.Fatal("expected run 1 to be purged after run 2 completed")
	}
	r2, _ := s.RunByID(ctx, id2)
	if r2.Status != StatusComplete {
		t.Fatalf("run 2 status = %s", r2.Status)
	}
}

func TestFailedRunPreservesPrevious(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	good, _ := s.BeginRun(ctx, "acme")
	if err := s.CompleteRun(ctx, good, RunCounts{Projects: 1}); err != nil {
		t.Fatalf("complete good: %v", err)
	}

	bad, _ := s.BeginRun(ctx, "acme")
	if err := s.FailRun(ctx, bad, StatusFailed, "collect: projects boom"); err != nil {
		t.Fatalf("FailRun: %v", err)
	}

	// Bad run marked failed; good run still present.
	br, _ := s.RunByID(ctx, bad)
	if br.Status != StatusFailed || br.Error == "" {
		t.Fatalf("bad run: %+v", br)
	}
	gr, err := s.RunByID(ctx, good)
	if err != nil {
		t.Fatalf("good run should be preserved: %v", err)
	}
	if gr.Status != StatusComplete {
		t.Fatalf("good run status = %s", gr.Status)
	}
}

func TestFailRunDiscardsData(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id, _ := s.BeginRun(ctx, "acme")
	tx, err := s.BeginTx(ctx, id)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := tx.AddProject(ctx, "p1", "Alpha"); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := tx.Abort(); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if err := s.FailRun(ctx, id, StatusFailed, "boom"); err != nil {
		t.Fatalf("FailRun: %v", err)
	}

	projs, err := s.ProjectsByRun(ctx, id)
	if err != nil {
		t.Fatalf("ProjectsByRun: %v", err)
	}
	if len(projs) != 0 {
		t.Fatalf("expected no projects after failed run, got %d", len(projs))
	}
}

func TestTxWritesAndCounts(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id, _ := s.BeginRun(ctx, "acme")
	tx, err := s.BeginTx(ctx, id)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := tx.AddProject(ctx, "p1", "Alpha"); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := tx.AddFolder(ctx, "p1", "/Shared"); err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	if err := tx.AddPipeline(ctx, "p1", 12, "CI", "/Shared", "enabled"); err != nil {
		t.Fatalf("AddPipeline: %v", err)
	}
	if err := tx.AddSubject(ctx, "s-1", "Alice", "aad", "user"); err != nil {
		t.Fatalf("AddSubject: %v", err)
	}
	if err := tx.AddAssignment(ctx, "p1/Shared/12", "g-1", 3, 0, false); err != nil {
		t.Fatalf("AddAssignment: %v", err)
	}
	if err := tx.AddMembership(ctx, "g-1", "s-1"); err != nil {
		t.Fatalf("AddMembership: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	c := tx.Counts()
	if c.Projects != 1 || c.Folders != 1 || c.Pipelines != 1 || c.Subjects != 1 || c.Assignments != 1 {
		t.Fatalf("unexpected counts: %+v", c)
	}

	// Duplicate insert should not double-count.
	tx2, _ := s.BeginTx(ctx, id)
	if err := tx2.AddProject(ctx, "p1", "Alpha"); err != nil {
		t.Fatalf("dup AddProject: %v", err)
	}
	if tx2.Counts().Projects != 0 {
		t.Fatalf("duplicate project should not count, got %d", tx2.Counts().Projects)
	}
	tx2.Commit()
}

func TestTokensByRun(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, id)
	tx.AddProject(ctx, "PROJ-A", "Alpha")
	tx.AddFolder(ctx, "PROJ-A", "/Shared")
	tx.AddPipeline(ctx, "PROJ-A", 12, "CI", "/Shared", "enabled")
	tx.AddFolder(ctx, "PROJ-A", "/")
	tx.Commit()

	toks, err := s.TokensByRun(ctx, id)
	if err != nil {
		t.Fatalf("TokensByRun: %v", err)
	}
	want := map[string]bool{
		"PROJ-A":           true, // project-level
		"PROJ-A/Shared":    true, // folder
		"PROJ-A/Shared/12": true, // pipeline in folder
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens %v, want %d", len(toks), toks, len(want))
	}
	for _, tk := range toks {
		if !want[tk] {
			t.Fatalf("unexpected token %q in %v", tk, toks)
		}
	}
}

func TestDeleteAll(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id, _ := s.BeginRun(ctx, "acme")
	s.CompleteRun(ctx, id, RunCounts{Projects: 1})

	if err := s.DeleteAll(ctx); err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	latest, _ := s.LatestRunID(ctx)
	if latest != 0 {
		t.Fatalf("expected no latest run after delete, got %d", latest)
	}
}

func TestDefaultDBPath(t *testing.T) {
	p := DefaultDBPath()
	if p == "" {
		t.Fatal("expected non-empty default path")
	}
}
