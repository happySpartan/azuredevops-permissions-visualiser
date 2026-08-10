package azdo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// SecurityNamespace describes an Azure DevOps security namespace.
type SecurityNamespace struct {
	NamespaceID string `json:"namespaceId"`
	Name        string `json:"name"`
	Actions     []ACE  `json:"actions"`
	Dataspace   string `json:"dataspace"`
	Separator   string `json:"separator"`
}

// ACE is a single permission action within a security namespace.
type ACE struct {
	Bit         int64  `json:"bit"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// ACLQuery is the request body for the ACL query endpoint.
type ACLQuery struct {
	SecurityTokens []string `json:"securityTokens"`
	Permissions    int64    `json:"permissions"`
	Recurse        bool     `json:"recurse"`
}

// ACL represents a single access control entry returned by the ACL query.
type ACL struct {
	Token     string  `json:"token"`
	Perm      int64   `json:"perm"`
	Inherited int64   `json:"inherited"`
	Entries   []ACLCE `json:"aces"`
}

// ACLCE is a single access control entry with a descriptor hash and deny/allow bits.
type ACLCE struct {
	Descriptor string `json:"descriptor"`
	Allow      int64  `json:"allow"`
	Deny       int64  `json:"deny"`
}

// BuildNamespaceID is the well-known GUID for the Build security namespace.
const BuildNamespaceID = "d34d3680-dfe5-4cc6-a949-7d9c68f73cba"

// SecurityNamespace retrieves a security namespace by its ID.
func (c *Client) SecurityNamespace(ctx context.Context, namespaceID string) (*SecurityNamespace, error) {
	q := apiVersion("7.1-preview.1")
	q.Set("localOnly", "false")
	out := &SecurityNamespace{}
	if err := c.get(ctx, "/_apis/securitynamespaces/"+namespaceID, q, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SecurityNamespaces lists all security namespaces.
func (c *Client) SecurityNamespaces(ctx context.Context) ([]SecurityNamespace, error) {
	var out struct {
		Value []SecurityNamespace `json:"value"`
	}
	q := apiVersion("7.1-preview.1")
	if err := c.get(ctx, "/_apis/securitynamespaces", q, &out); err != nil {
		return nil, err
	}
	return out.Value, nil
}

// ACEQuery queries access control entries for the given security tokens in a
// namespace. Returns a map from security token to its ACL. Azure DevOps limits
// requests to 500 tokens each, so large sets are chunked.
func (c *Client) ACEQuery(ctx context.Context, namespaceID string, tokens []string, recurse bool) (map[string]ACL, error) {
	const maxChunk = 500
	all := make(map[string]ACL)
	for i := 0; i < len(tokens); i += maxChunk {
		end := i + maxChunk
		if end > len(tokens) {
			end = len(tokens)
		}
		body := ACLQuery{
			SecurityTokens: tokens[i:end],
			Permissions:    -1, // fetch all bit permissions
			Recurse:        recurse,
		}
		q := url.Values{"api-version": {"7.1-preview.1"}}
		req, err := c.request(ctx, "POST", "/_apis/security/aclquery", q, body)
		if err != nil {
			return nil, err
		}
		resp, err := c.do(ctx, req)
		if err != nil {
			return nil, err
		}
		var out struct {
			Value []ACL `json:"value"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("azdo: decoding aclquery: %w", decodeErr)
		}
		for _, a := range out.Value {
			all[a.Token] = a
		}
	}
	return all, nil
}

// BuildSecurityToken returns the security token for a project-level Build
// permission, or a specific build definition, optionally within a folder path.
// Example: "00001111-aaaa-2222-bbbb-3333cccc4444" for project-level,
// "00001111-aaaa-2222-bbbb-3333cccc4444/12" for a specific definition,
// "00001111-aaaa-2222-bbbb-3333cccc4444/MyFolder/12" for folder-nested.
func BuildSecurityToken(projectID, folderPath string, definitionID int) string {
	tok := projectID
	// API folder paths come back with a leading slash (e.g. "/Shared"); trim it
	// so tokens don't contain a double slash.
	folderPath = strings.TrimPrefix(folderPath, "/")
	if folderPath != "" {
		tok += "/" + folderPath
	}
	if definitionID > 0 {
		tok += "/" + fmt.Sprint(definitionID)
	}
	return tok
}
