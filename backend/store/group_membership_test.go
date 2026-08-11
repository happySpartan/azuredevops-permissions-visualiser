package store

import (
	"context"
	"reflect"
	"testing"
)

func TestGroupMembershipByRunReturnsDirectAndTransitiveMembersWithPaths(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	for _, subject := range []struct{ descriptor, name, kind string }{
		{"g-root", "Engineering", "group"},
		{"g-platform", "Platform", "group"},
		{"u-alice", "Alice", "user"},
		{"u-bob", "Bob", "user"},
	} {
		_ = tx.AddSubject(ctx, subject.descriptor, subject.name, "aad", subject.kind)
	}
	_ = tx.AddMembership(ctx, "g-root", "g-platform")
	_ = tx.AddMembership(ctx, "g-root", "u-bob")
	_ = tx.AddMembership(ctx, "g-platform", "u-alice")
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	detail, err := s.GroupMembershipByRun(ctx, runID, "g-root")
	if err != nil {
		t.Fatalf("GroupMembershipByRun: %v", err)
	}
	if detail.Group.Descriptor != "g-root" {
		t.Fatalf("group = %+v", detail.Group)
	}

	got := make([]struct {
		descriptor string
		direct     bool
		path       []string
	}, 0, len(detail.Members))
	for _, member := range detail.Members {
		path := make([]string, len(member.Path))
		for i, subject := range member.Path {
			path[i] = subject.Descriptor
		}
		got = append(got, struct {
			descriptor string
			direct     bool
			path       []string
		}{member.Subject.Descriptor, member.Direct, path})
	}
	want := []struct {
		descriptor string
		direct     bool
		path       []string
	}{
		{"u-alice", false, []string{"g-root", "g-platform", "u-alice"}},
		{"u-bob", true, []string{"g-root", "u-bob"}},
		{"g-platform", true, []string{"g-root", "g-platform"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("members = %#v, want %#v", got, want)
	}
}

func TestGroupMembershipByRunHandlesCyclesAndChoosesDeterministicDisplayPath(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	for _, subject := range []struct{ descriptor, name, kind string }{
		{"g-root", "Root", "group"},
		{"g-a", "Zulu", "group"},
		{"g-z", "Alpha", "group"},
		{"u-one", "One", "user"},
	} {
		_ = tx.AddSubject(ctx, subject.descriptor, subject.name, "aad", subject.kind)
	}
	_ = tx.AddMembership(ctx, "g-root", "g-a")
	_ = tx.AddMembership(ctx, "g-root", "g-z")
	_ = tx.AddMembership(ctx, "g-a", "u-one")
	_ = tx.AddMembership(ctx, "g-z", "u-one")
	_ = tx.AddMembership(ctx, "g-a", "g-root") // malformed cycle must terminate
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	detail, err := s.GroupMembershipByRun(ctx, runID, "g-root")
	if err != nil {
		t.Fatalf("GroupMembershipByRun: %v", err)
	}
	if len(detail.Members) != 3 {
		t.Fatalf("members = %+v; root must not reappear through cycle", detail.Members)
	}
	for _, member := range detail.Members {
		if member.Subject.Descriptor != "u-one" {
			continue
		}
		got := []string{}
		for _, pathSubject := range member.Path {
			got = append(got, pathSubject.Descriptor)
		}
		want := []string{"g-root", "g-z", "u-one"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("path = %v, want display-name ordered path %v", got, want)
		}
		return
	}
	t.Fatal("transitive user not returned")
}

func TestGroupMembershipByRunRejectsNonGroup(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddSubject(ctx, "u-one", "One", "aad", "user")
	_ = tx.Commit()

	_, err := s.GroupMembershipByRun(ctx, runID, "u-one")
	if err != ErrNotFound {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
