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
	"time"

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

// Phase identifies the current step of a collection run.
type Phase string

const (
	PhaseAuthenticating Phase = "authenticating"
	PhaseProjects       Phase = "projects"
	PhaseBuilds         Phase = "builds"
	PhaseRepositories   Phase = "repositories"
	PhaseResources      Phase = "pipeline resources"
	PhaseSubjects       Phase = "subjects"
	PhasePermissions    Phase = "permissions"
	PhaseCommitting     Phase = "committing"
	PhaseComplete       Phase = "complete"
)

// Progress is emitted when the collector advances to a new phase.
type Progress struct {
	Phase   Phase  `json:"phase"`
	Message string `json:"message"`
}

// ProgressFunc receives collection progress. It must return quickly.
type ProgressFunc func(Progress)

// Collect performs a full one-shot collection. On success the run is committed
// and the previous successful run is replaced. On failure the run is marked
// failed and its partial data discarded.
func (c *Collector) Collect(ctx context.Context, org string) (*Result, error) {
	return c.CollectWithProgress(ctx, org, nil)
}

// CollectWithProgress performs a full collection and reports each phase.
func (c *Collector) CollectWithProgress(ctx context.Context, org string, report ProgressFunc) (*Result, error) {
	notify := func(phase Phase, message string) {
		if report != nil {
			report(Progress{Phase: phase, Message: message})
		}
	}
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
	notify(PhaseProjects, "Discovering projects")
	if err := c.collectProjects(ctx, tx); err != nil {
		tx.Abort()
		_ = c.fail(ctx, runID, store.StatusFailed, err)
		return nil, err
	}
	notify(PhaseBuilds, "Discovering pipeline folders and YAML pipelines")
	if err := c.collectBuilds(ctx, tx); err != nil {
		tx.Abort()
		_ = c.fail(ctx, runID, store.StatusFailed, err)
		return nil, err
	}
	notify(PhaseRepositories, "Discovering Git repositories and branches")
	if err := c.collectRepositories(ctx, tx); err != nil {
		tx.Abort()
		_ = c.fail(ctx, runID, store.StatusFailed, err)
		return nil, err
	}
	notify(PhaseResources, "Discovering agent pools, service connections, and variable groups")
	if err := c.collectPipelineResources(ctx, tx); err != nil {
		tx.Abort()
		_ = c.fail(ctx, runID, store.StatusFailed, err)
		return nil, err
	}
	notify(PhaseSubjects, "Discovering users, groups, and memberships")
	if err := c.collectSubjects(ctx, tx); err != nil {
		tx.Abort()
		_ = c.fail(ctx, runID, store.StatusFailed, err)
		return nil, err
	}
	notify(PhasePermissions, "Collecting Build and Git permission assignments")
	if err := c.collectPermissions(ctx, tx); err != nil {
		tx.Abort()
		_ = c.fail(ctx, runID, store.StatusFailed, err)
		return nil, err
	}

	counts := tx.Counts()
	notify(PhaseCommitting, "Committing the completed snapshot")
	if err := ctx.Err(); err != nil {
		tx.Abort()
		_ = c.fail(ctx, runID, store.StatusCancelled, err)
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		_ = c.fail(ctx, runID, store.StatusFailed, err)
		return nil, err
	}
	// Once the collected transaction commits, finalization must not be interrupted
	// or the database could retain a complete snapshot marked as still running.
	finalizeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.store.CompleteRun(finalizeContext, runID, counts); err != nil {
		return nil, err
	}
	notify(PhaseComplete, "Collection complete")
	return &Result{RunID: runID, Counts: counts}, nil
}

// fail marks a run failed/cancelled and discards its data.
func (c *Collector) fail(_ context.Context, runID int64, status store.Status, cause error) error {
	if errors.Is(cause, context.Canceled) {
		status = store.StatusCancelled
	}
	// The request context may already be cancelled. Cleanup must use a bounded
	// independent context so cancellation never leaves a running run behind.
	cleanupContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.store.FailRun(cleanupContext, runID, status, cause.Error())
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

// collectRepositories discovers Git repositories and their branches per project.
func (c *Collector) collectRepositories(ctx context.Context, tx *store.Tx) error {
	projects, err := tx.ProjectsByRunTx(ctx)
	if err != nil {
		return fmt.Errorf("collect: projects lookup: %w", err)
	}
	for _, p := range projects {
		repos, err := c.client.Repositories(ctx, p.Name)
		if err != nil {
			return fmt.Errorf("collect: repositories for %s: %w", p.Name, err)
		}
		for _, r := range repos {
			if err := tx.AddRepository(ctx, p.OrgID, r.ID, r.Name, r.DefaultBranch); err != nil {
				return err
			}
			branches, err := c.client.RepositoryBranches(ctx, p.Name, r.ID)
			if err != nil {
				return fmt.Errorf("collect: branches for %s/%s: %w", p.Name, r.Name, err)
			}
			for _, b := range branches {
				if err := tx.AddBranch(ctx, r.ID, b.Name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// collectPipelineResources discovers agent pools (org-level), service connections,
// and variable groups per project.
func (c *Collector) collectPipelineResources(ctx context.Context, tx *store.Tx) error {
	// Agent pools are org-level, collected once.
	pools, err := c.client.AgentPools(ctx)
	if err != nil {
		return fmt.Errorf("collect: agent pools: %w", err)
	}
	for _, p := range pools {
		if err := tx.AddAgentPool(ctx, p.ID, p.Name, p.IsHosted); err != nil {
			return err
		}
	}

	// Service connections and variable groups are per-project.
	projects, err := tx.ProjectsByRunTx(ctx)
	if err != nil {
		return fmt.Errorf("collect: projects lookup: %w", err)
	}
	for _, p := range projects {
		endpoints, err := c.client.ServiceEndpoints(ctx, p.Name)
		if err != nil {
			return fmt.Errorf("collect: endpoints for %s: %w", p.Name, err)
		}
		for _, e := range endpoints {
			if err := tx.AddServiceEndpoint(ctx, p.OrgID, e.ID, e.Name, e.Type); err != nil {
				return err
			}
		}
		vgs, err := c.client.VariableGroups(ctx, p.Name)
		if err != nil {
			return fmt.Errorf("collect: variable groups for %s: %w", p.Name, err)
		}
		for _, vg := range vgs {
			if err := tx.AddVariableGroup(ctx, p.OrgID, vg.ID, vg.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectPermissions queries the Build and Git security namespaces for every
// secured resource recorded in the run.
func (c *Collector) collectPermissions(ctx context.Context, tx *store.Tx) error {
	// Build namespace: projects, folders, pipelines.
	buildNS, err := c.client.SecurityNamespace(ctx, azdo.BuildNamespaceID)
	if err != nil {
		return fmt.Errorf("collect: build namespace: %w", err)
	}
	tokens, err := tx.TokensByRunTx(ctx)
	if err != nil {
		return fmt.Errorf("collect: tokens: %w", err)
	}
	if len(tokens) > 0 {
		for _, action := range buildNS.Actions {
			if err := tx.AddPermissionAction(ctx, azdo.BuildNamespaceID, action.Bit, action.Name, action.DisplayName); err != nil {
				return err
			}
		}
		if err := c.collectNamespaceACLs(ctx, tx, azdo.BuildNamespaceID, tokens); err != nil {
			return err
		}
	}

	// Git namespace: repositories and branches.
	gitNS, err := c.client.SecurityNamespace(ctx, azdo.GitNamespaceID)
	if err != nil {
		return fmt.Errorf("collect: git namespace: %w", err)
	}
	gitTokens, err := tx.GitTokensByRunTx(ctx)
	if err != nil {
		return fmt.Errorf("collect: git tokens: %w", err)
	}
	if len(gitTokens) > 0 {
		for _, action := range gitNS.Actions {
			if err := tx.AddPermissionAction(ctx, azdo.GitNamespaceID, action.Bit, action.Name, action.DisplayName); err != nil {
				return err
			}
		}
		if err := c.collectNamespaceACLs(ctx, tx, azdo.GitNamespaceID, gitTokens); err != nil {
			return err
		}
	}

	// Pipeline resource namespaces: agent pools, service connections, variable groups.
	resTokens, err := tx.PipelineResourceTokensByRunTx(ctx)
	if err != nil {
		return fmt.Errorf("collect: pipeline resource tokens: %w", err)
	}
	for _, ns := range []struct {
		id  string
		key string
	}{
		{azdo.BuildAdministrationNamespaceID, store.NamespaceBuildAdministration},
		{azdo.ServiceEndpointsNamespaceID, store.NamespaceServiceEndpoints},
		{azdo.LibraryNamespaceID, store.NamespaceLibrary},
	} {
		tokens := resTokens[ns.key]
		if len(tokens) == 0 {
			continue
		}
		resourceNS, err := c.client.SecurityNamespace(ctx, ns.id)
		if err != nil {
			return fmt.Errorf("collect: %s namespace: %w", ns.key, err)
		}
		for _, action := range resourceNS.Actions {
			if err := tx.AddPermissionAction(ctx, ns.id, action.Bit, action.Name, action.DisplayName); err != nil {
				return err
			}
		}
		if err := c.collectNamespaceACLs(ctx, tx, ns.id, tokens); err != nil {
			return err
		}
	}
	return nil
}

// collectNamespaceACLs queries one security namespace for the given tokens and
// persists its assignments and any ACL identities omitted from Graph listings.
func (c *Collector) collectNamespaceACLs(ctx context.Context, tx *store.Tx, namespaceID string, tokens []string) error {
	acls, err := c.client.ACEQuery(ctx, namespaceID, tokens, false)
	if err != nil {
		return fmt.Errorf("collect: aclquery %s: %w", namespaceID, err)
	}
	// ACL entries are keyed by Azure DevOps' general (storage) descriptor
	// (e.g. "Microsoft.TeamFoundation.Identity;S-1-9-..."), which is a
	// different encoding from the Graph descriptor used to identify subjects.
	// Resolve the distinct general descriptors to their Graph form so stored
	// assignments join subjects by the same key.
	var general []string
	for _, acl := range acls {
		for _, e := range acl.Entries {
			if e.Descriptor != "" {
				general = append(general, e.Descriptor)
			}
		}
	}
	identityByGeneral, err := c.client.IdentityGraphDescriptors(ctx, general)
	if err != nil {
		return fmt.Errorf("collect: resolve acl descriptors: %w", err)
	}
	// Some built-in collection groups are returned by the identity endpoint but
	// omitted from the Graph groups listing. Persist resolved ACL identities too
	// so every assignment can be explored through a subject.
	for _, identity := range identityByGeneral {
		if identity.Descriptor == "" {
			continue
		}
		if err := tx.AddSubject(ctx, identity.Descriptor, identity.DisplayName, identity.Origin, identity.SubjectKind); err != nil {
			return err
		}
	}
	// Persist entries using the Graph descriptor when resolvable.
	for token, acl := range acls {
		for _, e := range acl.Entries {
			graphDesc := e.Descriptor
			if identity, ok := identityByGeneral[e.Descriptor]; ok && identity.Descriptor != "" {
				graphDesc = identity.Descriptor
			}
			info := e.ExtendedInfo
			if err := tx.AddAssignmentExtended(ctx, namespaceID, token, graphDesc, e.Allow, e.Deny,
				info.InheritedAllow, info.InheritedDeny, info.EffectiveAllow, info.EffectiveDeny); err != nil {
				return err
			}
		}
	}
	return nil
}

// ErrNoActiveRun guards against misuse.
var ErrNoActiveRun = errors.New("collect: no active run")
