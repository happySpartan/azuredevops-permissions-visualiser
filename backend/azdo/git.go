package azdo

import "context"

// GitRepository is a Git repository within a project (Git namespace).
type GitRepository struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DefaultBranch string `json:"defaultBranch"`
	Project       struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"project"`
}

// GitBranch is a branch (a head ref) within a repository. Name carries the
// full ref, e.g. "refs/heads/main".
type GitBranch struct {
	Name string `json:"name"`
}

// GitNamespaceID is the well-known GUID for the Git Repositories security
// namespace, verified against a live Azure DevOps organization.
const GitNamespaceID = "2e9eb7ed-3c0a-47d4-87c1-0ffdd275fd87"

// Repositories lists all Git repositories in a project.
func (c *Client) Repositories(ctx context.Context, project string) ([]GitRepository, error) {
	var all []GitRepository
	q := apiVersion("7.1-preview.1")
	intQuery(q, "$top", 1000)
	for {
		var out struct {
			Value        []GitRepository   `json:"value"`
			Continuation *jsonContinuation `json:"continuationToken"`
		}
		if err := c.get(ctx, "/"+project+"/_apis/git/repositories", q, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Value...)
		if out.Continuation == nil || out.Continuation.Value == "" || len(out.Value) == 0 {
			break
		}
		q.Set("continuationToken", out.Continuation.Value)
	}
	return all, nil
}

// RepositoryBranches lists the head refs (branches) of a repository. Names are
// full refs such as "refs/heads/main".
func (c *Client) RepositoryBranches(ctx context.Context, project, repositoryID string) ([]GitBranch, error) {
	var all []GitBranch
	q := apiVersion("7.1-preview.1")
	q.Set("filter", "heads")
	// Azure DevOps paginates refs; loop on the continuation token until it is
	// absent or a page comes back short.
	for {
		var page struct {
			Value        []GitBranch       `json:"value"`
			Continuation *jsonContinuation `json:"continuationToken"`
		}
		if err := c.get(ctx, "/"+project+"/_apis/git/repositories/"+repositoryID+"/refs", q, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Value...)
		if page.Continuation == nil || page.Continuation.Value == "" || len(page.Value) == 0 {
			break
		}
		q.Set("continuationToken", page.Continuation.Value)
	}
	return all, nil
}

// GitRepoSecurityToken returns the Git-namespace security token for a
// repository. It is simply the repository GUID, which is distinct from project
// and definition tokens in other namespaces.
func GitRepoSecurityToken(repositoryID string) string {
	return repositoryID
}

// GitBranchSecurityToken returns the Git-namespace security token for a branch
// within a repository, e.g. "<repoGUID>/refs/heads/main".
func GitBranchSecurityToken(repositoryID, ref string) string {
	return repositoryID + "/" + ref
}
