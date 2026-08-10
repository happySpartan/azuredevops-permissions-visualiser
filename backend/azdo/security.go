package azdo

import (
	"context"
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
	Token              string           `json:"token"`
	InheritPermissions bool             `json:"inheritPermissions"`
	Entries            map[string]ACLCE `json:"acesDictionary"`
}

// ACLCE is a single access control entry with Azure DevOps extended information.
type ACLCE struct {
	Descriptor   string                 `json:"descriptor"`
	Allow        int64                  `json:"allow"`
	Deny         int64                  `json:"deny"`
	ExtendedInfo ACLExtendedInformation `json:"extendedInfo"`
}

// ACLExtendedInformation holds Azure DevOps' inherited and effective result.
type ACLExtendedInformation struct {
	InheritedAllow int64 `json:"inheritedAllow"`
	InheritedDeny  int64 `json:"inheritedDeny"`
	EffectiveAllow int64 `json:"effectiveAllow"`
	EffectiveDeny  int64 `json:"effectiveDeny"`
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
	all := make(map[string]ACL)
	for _, token := range tokens {
		q := url.Values{"api-version": {"7.1"}}
		q.Set("token", token)
		q.Set("includeExtendedInfo", "true")
		q.Set("recurse", fmt.Sprint(recurse))
		var out []ACL
		if err := c.get(ctx, "/_apis/accesscontrollists/"+namespaceID, q, &out); err != nil {
			return nil, err
		}
		for _, acl := range out {
			all[acl.Token] = acl
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
