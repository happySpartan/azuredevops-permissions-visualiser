package collect

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/store"
)

func TestManagerRejectsConcurrentAttempt(t *testing.T) {
	manager := NewManager()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	run := func(ctx context.Context, report ProgressFunc) (*Result, error) {
		calls.Add(1)
		close(started)
		<-release
		return &Result{RunID: 42}, nil
	}

	if err := manager.Start(context.Background(), run); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	<-started
	if err := manager.Start(context.Background(), run); !errors.Is(err, ErrCollectionRunning) {
		t.Fatalf("second Start error = %v, want ErrCollectionRunning", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("runner calls = %d, want 1", calls.Load())
	}
	close(release)
	waitForState(t, manager, StateSucceeded)
}

func TestManagerPublishesProgressAndResult(t *testing.T) {
	manager := NewManager()
	run := func(ctx context.Context, report ProgressFunc) (*Result, error) {
		report(Progress{Phase: PhaseSubjects, Message: "Discovering subjects"})
		return &Result{RunID: 7, Counts: store.RunCounts{Subjects: 12}}, nil
	}
	if err := manager.Start(context.Background(), run); err != nil {
		t.Fatalf("Start: %v", err)
	}
	status := waitForState(t, manager, StateSucceeded)
	if status.Phase != PhaseComplete || status.RunID != 7 || status.Counts.Subjects != 12 {
		t.Fatalf("status = %+v", status)
	}
}

func TestManagerPublishesActionableFailure(t *testing.T) {
	manager := NewManager()
	want := errors.New("Azure CLI authentication failed; run `az login` and try again")
	if err := manager.Start(context.Background(), func(context.Context, ProgressFunc) (*Result, error) {
		return nil, want
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	status := waitForState(t, manager, StateFailed)
	if status.Error != want.Error() {
		t.Fatalf("error = %q, want %q", status.Error, want)
	}
}

func waitForState(t *testing.T, manager *Manager, want State) Status {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := manager.Status()
		if status.State == want {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; latest: %+v", want, manager.Status())
	return Status{}
}
