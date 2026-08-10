package azdo

import "context"

// GraphSubject is a user or group in the Graph API.
type GraphSubject struct {
	DisplayName string `json:"displayName"`
	Descriptor  string `json:"descriptor"`
	Origin      string `json:"origin"` // "aad" (Entra), "vsts" (Azure DevOps), "ims", etc.
	OriginID    string `json:"originId"`
	SubjectKind string `json:"subjectKind"` // "user" or "group"
	URL         string `json:"url,omitempty"`
}

// GraphSubjects lists users or groups in the organization.
func (c *Client) GraphSubjects(ctx context.Context, subjectKind string) ([]GraphSubject, error) {
	endpoint := "/_apis/graph/users"
	if subjectKind == "group" {
		endpoint = "/_apis/graph/groups"
	}
	var all []GraphSubject
	q := apiVersion("7.1-preview.1")
	q.Set("scopeDescriptor", "")
	q.Set("subjectTypes", subjectKind)
	for {
		var out struct {
			Value        []GraphSubject    `json:"value"`
			Continuation *jsonContinuation `json:"continuationToken"`
		}
		if err := c.get(ctx, endpoint, q, &out); err != nil {
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

// GraphMemberships lists the members of a subject (by descriptor), returning
// their descriptors. It follows the Graph memberships API.
func (c *Client) GraphMemberships(ctx context.Context, subjectDescriptor string) ([]string, error) {
	var out struct {
		Value []struct {
			MembershipID string `json:"membershipId"`
		} `json:"value"`
	}
	q := apiVersion("7.1-preview.1")
	q.Set("direction", "down")
	q.Set("depth", "1") // direct members only; caller expands nested on demand
	if err := c.get(ctx, "/_apis/graph/memberships/"+subjectDescriptor, q, &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Value))
	for _, m := range out.Value {
		if m.MembershipID != "" {
			ids = append(ids, m.MembershipID)
		}
	}
	return ids, nil
}
