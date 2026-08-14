package azdo

import (
	"context"
	"strconv"
)

// AgentPool is an organization-level agent pool (BuildAdministration namespace).
type AgentPool struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	IsHosted bool   `json:"isHosted"`
	PoolType string `json:"poolType"`
}

// ServiceEndpoint is a service connection (ServiceEndpoints namespace).
type ServiceEndpoint struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	IsShared bool  `json:"isShared"`
}

// VariableGroup is a library variable group (Library namespace).
type VariableGroup struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// Pipeline resource security namespaces, verified against a live organization.
const (
	// BuildAdministrationNamespaceID covers agent pools.
	BuildAdministrationNamespaceID = "302acaca-b667-436d-a946-87133492041c"
	// ServiceEndpointsNamespaceID covers service connections.
	ServiceEndpointsNamespaceID = "49b48001-ca20-4adc-8111-5b60c903a50c"
	// LibraryNamespaceID covers variable groups (and other library items).
	LibraryNamespaceID = "b7e84409-6553-448a-bbb2-af228e07cbeb"
)

// AgentPools lists all agent pools in the organization.
func (c *Client) AgentPools(ctx context.Context) ([]AgentPool, error) {
	var out struct {
		Value []AgentPool `json:"value"`
	}
	q := apiVersion("7.1-preview.1")
	intQuery(q, "$top", 1000)
	if err := c.get(ctx, "/_apis/distributedtask/pools", q, &out); err != nil {
		return nil, err
	}
	return out.Value, nil
}

// ServiceEndpoints lists the service connections in a project.
func (c *Client) ServiceEndpoints(ctx context.Context, project string) ([]ServiceEndpoint, error) {
	var out struct {
		Value []ServiceEndpoint `json:"value"`
	}
	q := apiVersion("7.1-preview.4")
	intQuery(q, "$top", 1000)
	if err := c.get(ctx, "/"+project+"/_apis/serviceendpoint/endpoints", q, &out); err != nil {
		return nil, err
	}
	return out.Value, nil
}

// VariableGroups lists the variable groups in a project.
func (c *Client) VariableGroups(ctx context.Context, project string) ([]VariableGroup, error) {
	var out struct {
		Value []VariableGroup `json:"value"`
	}
	q := apiVersion("7.1-preview.2")
	intQuery(q, "$top", 1000)
	if err := c.get(ctx, "/"+project+"/_apis/distributedtask/variablegroups", q, &out); err != nil {
		return nil, err
	}
	return out.Value, nil
}

// AgentPoolSecurityToken returns the BuildAdministration-namespace security
// token for an agent pool, e.g. "pools/1".
func AgentPoolSecurityToken(poolID int) string {
	return "pools/" + strconv.Itoa(poolID)
}

// ServiceEndpointSecurityToken returns the ServiceEndpoints-namespace security
// token for a service connection in a project, e.g. "<projectGUID>/<endpointGUID>".
func ServiceEndpointSecurityToken(projectID, endpointID string) string {
	return projectID + "/" + endpointID
}

// VariableGroupSecurityToken returns the Library-namespace security token for
// a variable group in a project, e.g. "<projectGUID>/<variableGroupId>".
func VariableGroupSecurityToken(projectID string, variableGroupID int) string {
	return projectID + "/" + strconv.Itoa(variableGroupID)
}
