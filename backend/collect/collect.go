// Package collect orchestrates a one-shot, point-in-time analysis run against
// an Azure DevOps organization, writing results atomically to the store.
//
// It honours the product decisions:
//
//   - One-shot collection: a single synchronous pass over the organization.
//   - "No half data": every phase must succeed. If any required phase fails,
//     the run is marked failed/cancelled and its partial data is discarded; the
//     previous successful run is preserved.
//   - Only on full success is the run committed and the previous run replaced.
//   - Azure DevOps is the source of truth: the collector records what the API
//     returns; it does not compute effective-permission verdicts.
package collect

import (
	"context"
	"errors"
	"fmt"

	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/azdo"
	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/store"
)

// Collector runs an analysis collection for one organization.
type Collector struct {
	client *azdo.Client
	store  *store.Store
}

// New constructs a Collector.
func New(client *azdo.Client, st *store.Store) *Collector {
	return &Collector{client: client, store: st}
}

// Result describes a completed collection.
type Result struct {
	RunID  int64
	Counts store.RunCounts
}

// Collect performs a full one-shot collection. On success the run is committed
// and the previous successful run is replaced. On failure the run is marked
// failed and its partial data discarded.
func (c *Collector) Collect(ctx context.Context, org string) (*Result, error) {
	runID, err := c.store.BeginRun(ctx, org)
	if err != nil {
		return nil, err
	}

	tx, err := c.store.BeginTx(ctx, runID)
	if err != nil {
		_ = c.fail(ctx, runID, store.StatusFailed, err)
		return nil, err
	}

	// Phases run in order; the first error aborts the whole run.
	if err := c.collectProjects(ctx, tx); err != nil {
		tx.Abort()
		_ = c.fail(ctx, runID, store.StatusFailed, err)
		return nil, err
	}
	if err := c.collectBuilds(ctx, tx); err != nil {
		tx.Abort()
		_ = c.fail(ctx, runID, store.StatusFailed, err)
		return nil, err
	}
	if err := c.collectSubjects(ctx, tx); err != nil {
		tx.Abort()
		_ = c.fail(ctx, runID, store.StatusFailed, err)
		return nil, err
	}
	if err := c.collectACLs(ctx, tx); err != nil {
		tx.Abort()
		_ = c.fail(ctx, runID, store.StatusFailed, err)
		return nil, err
	}

	counts := tx.Counts()
	if err := tx.Commit(); err != nil {
		_ = c.fail(ctx, runID, store.StatusFailed, err)
		return nil, err
	}
	if err := c.store.CompleteRun(ctx, runID, counts); err != nil {
		return nil, err
	}
	return &Result{RunID: runID, Counts: counts}, nil
}

// fail marks a run failed/cancelled and discards its data.
func (c *Collector) fail(ctx context.Context, runID int64, status store.Status, cause error) error {
	return c.store.FailRun(ctx, runID, status, cause.Error())
}

// collectProjects discovers all projects in the organization.
func (c *Collector) collectProjects(ctx context.Context, tx *store.Tx) error {
	projects, err := c.client.Projects(ctx)
	if err != nil {
		return fmt.Errorf("collect: projects: %w", err)
	}
	for _, p := range projects {
		if err := tx.AddProject(ctx, p.ID, p.Name); err != nil {
			return err
		}
	}
	return nil
}

// collectBuilds discovers pipeline folders and YAML pipeline definitions per project.
func (c *Collector) collectBuilds(ctx context.Context, tx *store.Tx) error {
	projects, err := tx.ProjectsByRunTx(ctx)
	if err != nil {
		return fmt.Errorf("collect: projects lookup: %w", err)
	}
	for _, p := range projects {
		folders, err := c.client.BuildFolders(ctx, p.Name)
		if err != nil {
			return fmt.Errorf("collect: folders for %s: %w", p.Name, err)
		}
		for _, f := range folders {
			if err := tx.AddFolder(ctx, p.OrgID, f.Path); err != nil {
				return err
			}
		}

		defs, err := c.client.BuildDefinitions(ctx, p.Name, false)
		if err != nil {
			return fmt.Errorf("collect: definitions for %s: %w", p.Name, err)
		}
		for _, d := range defs {
			if err := tx.AddPipeline(ctx, p.OrgID, d.ID, d.Name, d.Path, d.QueueStatus); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectSubjects discovers users and groups and their direct memberships.
func (c *Collector) collectSubjects(ctx context.Context, tx *store.Tx) error {
	users, err := c.client.GraphSubjects(ctx, "user")
	if err != nil {
		return fmt.Errorf("collect: users: %w", err)
	}
	for _, u := range users {
		if err := tx.AddSubject(ctx, u.Descriptor, u.DisplayName, u.Origin, u.SubjectKind); err != nil {
			return err
		}
	}

	groups, err := c.client.GraphSubjects(ctx, "group")
	if err != nil {
		return fmt.Errorf("collect: groups: %w", err)
	}
	for _, g := range groups {
		if err := tx.AddSubject(ctx, g.Descriptor, g.DisplayName, g.Origin, g.SubjectKind); err != nil {
			return err
		}
		// Direct memberships only; callers expand nested members on demand.
		members, err := c.client.GraphMemberships(ctx, g.Descriptor)
		if err != nil {
			return fmt.Errorf("collect: memberships for %s: %w", g.DisplayName, err)
		}
		for _, m := range members {
			if err := tx.AddMembership(ctx, g.Descriptor, m); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectACLs queries the Build security namespace for every secured resource
// recorded in the run (projects, folders, and pipelines).
func (c *Collector) collectACLs(ctx context.Context, tx *store.Tx) error {
	ns, err := c.client.SecurityNamespace(ctx, azdo.BuildNamespaceID)
	if err != nil {
		return fmt.Errorf("collect: build namespace: %w", err)
	}
	_ = ns // namespace actions are used by the UI; collection records raw ACLs

	tokens, err := tx.TokensByRunTx(ctx)
	if err != nil {
		return fmt.Errorf("collect: tokens: %w", err)
	}
	if len(tokens) == 0 {
		return nil
	}
	acls, err := c.client.ACEQuery(ctx, azdo.BuildNamespaceID, tokens, false)
	if err != nil {
		return fmt.Errorf("collect: aclquery: %w", err)
	}
	// Persist raw entries as reported by Azure DevOps.
	for token, acl := range acls {
		for _, e := range acl.Entries {
			if err := tx.AddAssignment(ctx, token, e.Descriptor, e.Allow, e.Deny, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// ErrNoActiveRun guards against misuse.
var ErrNoActiveRun = errors.New("collect: no active run")
