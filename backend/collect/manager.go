package collect

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/store"
)

// ErrCollectionRunning is returned when a collection is already in progress.
var ErrCollectionRunning = errors.New("a collection is already running")

// ErrCollectionNotRunning is returned when cancellation is requested while idle.
var ErrCollectionNotRunning = errors.New("no collection is running")

// State is the lifecycle state exposed by the collection status endpoint.
type State string

const (
	StateIdle      State = "idle"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// Status is a safe snapshot of the latest collection attempt.
type Status struct {
	State     State           `json:"state"`
	Phase     Phase           `json:"phase,omitempty"`
	Message   string          `json:"message,omitempty"`
	Error     string          `json:"error,omitempty"`
	RunID     int64           `json:"runID,omitempty"`
	Counts    store.RunCounts `json:"counts"`
	StartedAt time.Time       `json:"startedAt,omitempty"`
	UpdatedAt time.Time       `json:"updatedAt,omitempty"`
}

// RunFunc executes one collection attempt.
type RunFunc func(context.Context, ProgressFunc) (*Result, error)

// Manager serializes collection attempts and publishes their current status.
type Manager struct {
	mu     sync.RWMutex
	status Status
	cancel context.CancelFunc
}

// NewManager returns an idle collection manager.
func NewManager() *Manager {
	return &Manager{status: Status{State: StateIdle}}
}

// Start launches a collection in the background. Only one attempt may run at a
// time. The runner deliberately outlives the initiating HTTP request.
func (m *Manager) Start(_ context.Context, run RunFunc) error {
	m.mu.Lock()
	if m.status.State == StateRunning {
		m.mu.Unlock()
		return ErrCollectionRunning
	}
	now := time.Now().UTC()
	m.status = Status{
		State:     StateRunning,
		Message:   "Preparing collection",
		StartedAt: now,
		UpdatedAt: now,
	}
	runContext, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.mu.Unlock()

	go func() {
		result, err := run(runContext, m.report)
		now := time.Now().UTC()
		m.mu.Lock()
		defer m.mu.Unlock()
		m.cancel = nil
		m.status.UpdatedAt = now
		if err != nil {
			if errors.Is(err, context.Canceled) {
				m.status.State = StateCancelled
				m.status.Message = "Collection cancelled"
				m.status.Error = ""
				return
			}
			m.status.State = StateFailed
			m.status.Message = "Collection failed"
			m.status.Error = err.Error()
			return
		}
		m.status.State = StateSucceeded
		m.status.Phase = PhaseComplete
		m.status.Message = "Collection complete"
		m.status.RunID = result.RunID
		m.status.Counts = result.Counts
	}()
	return nil
}

// Cancel asks the active collection to stop and returns immediately.
func (m *Manager) Cancel() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.State != StateRunning || m.cancel == nil {
		return ErrCollectionNotRunning
	}
	m.cancel()
	return nil
}

func (m *Manager) report(progress Progress) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.State != StateRunning {
		return
	}
	m.status.Phase = progress.Phase
	m.status.Message = progress.Message
	m.status.UpdatedAt = time.Now().UTC()
}

// Status returns a race-free copy of the latest attempt status.
func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}
