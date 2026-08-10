package azdo

import "context"

// Project is a minimal Azure DevOps project.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	State       string `json:"state"`
}

// Projects lists all projects in the organization.
func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	var out struct {
		Value []Project `json:"value"`
	}
	q := apiVersion("7.1-preview.4")
	intQuery(q, "stateFilter", 1) // all non-deleted projects
	intQuery(q, "top", 1000)
	if err := c.get(ctx, "/_apis/projects", q, &out); err != nil {
		return nil, err
	}
	return out.Value, nil
}
