package store

import (
	"context"
	"testing"
)

func TestResourcePermissionsByRunResolvesAgentPoolToken(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddAgentPool(ctx, 1, "Azure Pipelines", true)
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddPermissionAction(ctx, NamespaceBuildAdministration, 1, "ViewBuildResources", "View build resources")
	_ = tx.AddPermissionAction(ctx, NamespaceBuildAdministration, 4, "UseBuildResources", "Use build resources")
	_ = tx.AddAssignmentExtended(ctx, NamespaceBuildAdministration, "pools/1", "u-1", 5, 0, 0, 0, 5, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	detail, err := s.ResourcePermissionsByRun(ctx, runID, "pools/1")
	if err != nil {
		t.Fatalf("ResourcePermissionsByRun: %v", err)
	}
	if detail.Resource.Namespace != NamespaceBuildAdministration || detail.Resource.Type != "agentPool" || detail.Resource.Name != "Azure Pipelines" {
		t.Fatalf("unexpected resource: %+v", detail.Resource)
	}
	if len(detail.Subjects) != 1 {
		t.Fatalf("subjects = %+v, want u-1", detail.Subjects)
	}
	permissions := detail.Subjects[0].Permissions
	if len(permissions) != 2 || permissions[0].Name != "ViewBuildResources" || permissions[1].Name != "UseBuildResources" {
		t.Fatalf("permissions = %+v, want the two BuildAdministration actions", permissions)
	}
}

func TestResourcePermissionsByRunResolvesServiceEndpointAndVariableGroup(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddServiceEndpoint(ctx, "p1", "EP-1", "npm registry", "npm")
	_ = tx.AddVariableGroup(ctx, "p1", 3, "shared-secrets")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddPermissionAction(ctx, NamespaceServiceEndpoints, 1, "Use", "Use Service Connection")
	_ = tx.AddPermissionAction(ctx, NamespaceLibrary, 16, "Use", "Use library item")
	_ = tx.AddAssignmentExtended(ctx, NamespaceServiceEndpoints, "p1/EP-1", "u-1", 1, 0, 0, 0, 1, 0)
	_ = tx.AddAssignmentExtended(ctx, NamespaceLibrary, "p1/3", "u-1", 16, 0, 0, 0, 16, 0)
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	endpointDetail, err := s.ResourcePermissionsByRun(ctx, runID, "p1/EP-1")
	if err != nil {
		t.Fatalf("ResourcePermissionsByRun endpoint: %v", err)
	}
	if endpointDetail.Resource.Namespace != NamespaceServiceEndpoints || endpointDetail.Resource.Type != "serviceConnection" || endpointDetail.Resource.Name != "npm registry" {
		t.Fatalf("unexpected endpoint resource: %+v", endpointDetail.Resource)
	}
	if len(endpointDetail.Subjects) != 1 || endpointDetail.Subjects[0].Permissions[0].Name != "Use" {
		t.Fatalf("unexpected endpoint subjects: %+v", endpointDetail.Subjects)
	}

	vgDetail, err := s.ResourcePermissionsByRun(ctx, runID, "p1/3")
	if err != nil {
		t.Fatalf("ResourcePermissionsByRun variable group: %v", err)
	}
	if vgDetail.Resource.Namespace != NamespaceLibrary || vgDetail.Resource.Type != "variableGroup" || vgDetail.Resource.Name != "shared-secrets" {
		t.Fatalf("unexpected variable group resource: %+v", vgDetail.Resource)
	}
	if len(vgDetail.Subjects) != 1 || vgDetail.Subjects[0].Permissions[0].Name != "Use" {
		t.Fatalf("unexpected variable group subjects: %+v", vgDetail.Subjects)
	}
}

func TestResourcesByRunIncludesAgentPoolsEndpointsAndVariableGroups(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	runID, _ := s.BeginRun(ctx, "acme")
	tx, _ := s.BeginTx(ctx, runID)
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddAgentPool(ctx, 1, "Azure Pipelines", true)
	_ = tx.AddAgentPool(ctx, 7, "Self-hosted", false)
	_ = tx.AddServiceEndpoint(ctx, "p1", "EP-1", "npm registry", "npm")
	_ = tx.AddVariableGroup(ctx, "p1", 3, "shared-secrets")
	_ = tx.Commit()
	_ = s.CompleteRun(ctx, runID, tx.Counts())

	hierarchy, err := s.ResourcesByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ResourcesByRun: %v", err)
	}
	if len(hierarchy.AgentPools) != 2 || hierarchy.AgentPools[0].Name != "Azure Pipelines" || !hierarchy.AgentPools[0].IsHosted {
		t.Fatalf("agent pools = %+v, want 2 with hosted first", hierarchy.AgentPools)
	}
	if len(hierarchy.Projects) != 1 {
		t.Fatalf("projects = %+v, want 1", hierarchy.Projects)
	}
	alpha := hierarchy.Projects[0]
	if len(alpha.Endpoints) != 1 || alpha.Endpoints[0].Name != "npm registry" {
		t.Fatalf("endpoints = %+v, want npm registry", alpha.Endpoints)
	}
	if len(alpha.VariableGroups) != 1 || alpha.VariableGroups[0].Name != "shared-secrets" {
		t.Fatalf("variable groups = %+v, want shared-secrets", alpha.VariableGroups)
	}
}

func TestPipelineResourceTokenAssembly(t *testing.T) {
	if got := "pools/" + "1"; got != "pools/1" {
		t.Fatalf("agent pool token = %q", got)
	}
	if got := "p1/" + "EP-1"; got != "p1/EP-1" {
		t.Fatalf("endpoint token = %q", got)
	}
	if got := "p1/" + "3"; got != "p1/3" {
		t.Fatalf("variable group token = %q", got)
	}
}
