package azdo

import "context"

// BuildFolder is a pipeline folder within a project's Build namespace.
type BuildFolder struct {
	Path    string `json:"path"`
	Project struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"project"`
}

// BuildDefinition is a build/pipeline definition. YAML pipelines are identified
// by Process.Type == 2 ("yaml").
type BuildDefinition struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"` // folder path, "/" for root
	QueueStatus string `json:"queueStatus"`
	Revision    int    `json:"revision"`
	Process     struct {
		Type int `json:"type"` // 2 == yaml, 1 == designer (classic)
	} `json:"process"`
	Project struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"project"`
}

// ProcessType constants for build definition process types.
const (
	ProcessTypeDesigner = 1
	ProcessTypeYAML     = 2
)

// BuildFolders lists pipeline folders in a project.
func (c *Client) BuildFolders(ctx context.Context, project string) ([]BuildFolder, error) {
	var out struct {
		Value []BuildFolder `json:"value"`
	}
	q := apiVersion("7.1-preview.2")
	q.Set("path", "/")
	if err := c.get(ctx, "/"+project+"/_apis/build/folders", q, &out); err != nil {
		return nil, err
	}
	return out.Value, nil
}

// BuildDefinitions lists all build/pipeline definitions in a project.
// It returns only YAML pipeline definitions unless includeNonYAML is true.
func (c *Client) BuildDefinitions(ctx context.Context, project string, includeNonYAML bool) ([]BuildDefinition, error) {
	var all []BuildDefinition
	q := apiVersion("7.1-preview.7")
	q.Set("$top", "1000")
	q.Set("includeAllProperties", "true")
	// Loop until a page comes back short (pagination via continuation token).
	for {
		var out struct {
			Count        int               `json:"count"`
			Value        []BuildDefinition `json:"value"`
			Continuation *jsonContinuation `json:"continuationToken"`
		}
		if err := c.get(ctx, "/"+project+"/_apis/build/definitions", q, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Value...)
		if out.Continuation == nil || out.Continuation.Value == "" || len(out.Value) == 0 {
			break
		}
		q.Set("continuationToken", out.Continuation.Value)
	}
	if includeNonYAML {
		return all, nil
	}
	// Keep only YAML pipelines.
	yaml := all[:0]
	for _, d := range all {
		if d.Process.Type == ProcessTypeYAML {
			yaml = append(yaml, d)
		}
	}
	return yaml, nil
}

// jsonContinuation models the continuation token returned by the API.
type jsonContinuation struct {
	Value string `json:"value"`
}
