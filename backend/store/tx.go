package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNoActiveRun indicates data was written outside a begun run's transaction.
var ErrNoActiveRun = errors.New("store: no active run transaction")

// Tx is a single run's data transaction. The collector calls its writers and
// then Commit (success) or Abort (failure/cancel). All writes for a run happen
// here so the run is atomic.
type Tx struct {
	db     *sql.DB
	tx     *sql.Tx
	runID  int64
	counts RunCounts
}

// BeginTx opens a transaction scoped to runID for writing its collected data.
func (s *Store) BeginTx(ctx context.Context, runID int64) (*Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx: %w", err)
	}
	return &Tx{db: s.db, tx: tx, runID: runID}, nil
}

// RunID returns the run this transaction writes to.
func (t *Tx) RunID() int64 { return t.runID }

// Counts returns the running tallies of writes so far.
func (t *Tx) Counts() RunCounts { return t.counts }

// AddProject records a project discovered in the org.
func (t *Tx) AddProject(ctx context.Context, orgID, name string) error {
	res, err := t.tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO projects (run_id, org_id, name) VALUES (?, ?, ?)`,
		t.runID, orgID, name)
	if err != nil {
		return fmt.Errorf("store: add project: %w", err)
	}
	return t.countIfChanged(res, &t.counts.Projects)
}

// AddFolder records a pipeline folder for a project.
func (t *Tx) AddFolder(ctx context.Context, projectID, path string) error {
	res, err := t.tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO folders (run_id, project_id, path) VALUES (?, ?, ?)`,
		t.runID, projectID, path)
	if err != nil {
		return fmt.Errorf("store: add folder: %w", err)
	}
	return t.countIfChanged(res, &t.counts.Folders)
}

// AddPipeline records a YAML pipeline definition for a project.
func (t *Tx) AddPipeline(ctx context.Context, projectID string, definitionID int, name, folderPath, queueStatus string) error {
	res, err := t.tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO pipelines (run_id, project_id, definition_id, name, folder_path, queue_status)
		VALUES (?, ?, ?, ?, ?, ?)`,
		t.runID, projectID, definitionID, name, folderPath, queueStatus)
	if err != nil {
		return fmt.Errorf("store: add pipeline: %w", err)
	}
	return t.countIfChanged(res, &t.counts.Pipelines)
}

// AddRepository records a Git repository for a project.
func (t *Tx) AddRepository(ctx context.Context, projectID, repositoryID, name, defaultBranch string) error {
	res, err := t.tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO repositories (run_id, project_id, repository_id, name, default_branch)
		VALUES (?, ?, ?, ?, ?)`,
		t.runID, projectID, repositoryID, name, defaultBranch)
	if err != nil {
		return fmt.Errorf("store: add repository: %w", err)
	}
	return t.countIfChanged(res, &t.counts.Repositories)
}

// AddBranch records a branch within a repository.
func (t *Tx) AddBranch(ctx context.Context, repositoryID, name string) error {
	res, err := t.tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO branches (run_id, repository_id, name)
		VALUES (?, ?, ?)`,
		t.runID, repositoryID, name)
	if err != nil {
		return fmt.Errorf("store: add branch: %w", err)
	}
	return t.countIfChanged(res, &t.counts.Branches)
}

// AddSubject records a user or group.
func (t *Tx) AddSubject(ctx context.Context, descriptor, displayName, origin, subjectKind string) error {
	res, err := t.tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO subjects (run_id, descriptor, display_name, origin, subject_kind)
		VALUES (?, ?, ?, ?, ?)`,
		t.runID, descriptor, displayName, origin, subjectKind)
	if err != nil {
		return fmt.Errorf("store: add subject: %w", err)
	}
	return t.countIfChanged(res, &t.counts.Subjects)
}

// AddMembership records that parent contains member (direct edge).
func (t *Tx) AddMembership(ctx context.Context, parentDescriptor, memberDescriptor string) error {
	res, err := t.tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO memberships (run_id, parent_descriptor, member_descriptor)
		VALUES (?, ?, ?)`,
		t.runID, parentDescriptor, memberDescriptor)
	if err != nil {
		return fmt.Errorf("store: add membership: %w", err)
	}
	return t.countIfChanged(res, nil)
}

// AddAssignment records a raw ACL entry for a security token and descriptor.
func (t *Tx) AddAssignment(ctx context.Context, securityToken, descriptor string, allow, deny int64, inherited bool) error {
	inh := 0
	if inherited {
		inh = 1
	}
	res, err := t.tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO assignments (run_id, security_token, descriptor, allow_bitmask, deny_bitmask, inherited)
		VALUES (?, ?, ?, ?, ?, ?)`,
		t.runID, securityToken, descriptor, allow, deny, inh)
	if err != nil {
		return fmt.Errorf("store: add assignment: %w", err)
	}
	return t.countIfChanged(res, &t.counts.Assignments)
}

// AddAssignmentExtended records explicit, inherited, and effective masks as
// reported by Azure DevOps for one access control entry in namespace ns.
func (t *Tx) AddAssignmentExtended(ctx context.Context, ns, securityToken, descriptor string, allow, deny, inheritedAllow, inheritedDeny, effectiveAllow, effectiveDeny int64) error {
	res, err := t.tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO assignments
		(run_id, namespace, security_token, descriptor, allow_bitmask, deny_bitmask, inherited,
		 inherited_allow_bitmask, inherited_deny_bitmask, effective_allow_bitmask, effective_deny_bitmask)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.runID, ns, securityToken, descriptor, allow, deny, inheritedAllow|inheritedDeny != 0,
		inheritedAllow, inheritedDeny, effectiveAllow, effectiveDeny)
	if err != nil {
		return fmt.Errorf("store: add extended assignment: %w", err)
	}
	return t.countIfChanged(res, &t.counts.Assignments)
}

// AddPermissionAction records one action from a security namespace.
func (t *Tx) AddPermissionAction(ctx context.Context, ns string, bit int64, name, displayName string) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO permission_actions (run_id, namespace, bit, name, display_name)
		VALUES (?, ?, ?, ?, ?)`, t.runID, ns, bit, name, displayName)
	if err != nil {
		return fmt.Errorf("store: add permission action: %w", err)
	}
	return nil
}

// Commit finalises the run's data transaction. It does not change run status;
// the collector calls store.CompleteRun separately for the atomic replace.
func (t *Tx) Commit() error { return t.tx.Commit() }

// Abort discards the run's transaction without applying any writes.
func (t *Tx) Abort() error { return t.tx.Rollback() }

// countIfChanged increments the appropriate counter only when a row was actually
// inserted (INSERT OR IGNORE may insert nothing on duplicate).
func (t *Tx) countIfChanged(res sql.Result, counter *int) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if counter != nil && n > 0 {
		*counter += int(n)
	}
	return nil
}
