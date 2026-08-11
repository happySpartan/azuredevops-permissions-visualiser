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

// State is the lifecycle state exposed by the collection status endpoint.
type State string

const (
	StateIdle      State = "idle"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
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
	m.mu.Unlock()

	go func() {
		result, err := run(context.Background(), m.report)
		now := time.Now().UTC()
		m.mu.Lock()
		defer m.mu.Unlock()
		m.status.UpdatedAt = now
		if err != nil {
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
