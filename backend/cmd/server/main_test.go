package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/collect"
	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/store"
)

func exportTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	runID, err := st.BeginRun(ctx, "acme")
	if err != nil {
		t.Fatalf("begin run: %v", err)
	}
	tx, err := st.BeginTx(ctx, runID)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	_ = tx.AddProject(ctx, "p1", "Alpha")
	_ = tx.AddSubject(ctx, "u-1", "Alice", "aad", "user")
	_ = tx.AddPermissionAction(ctx, 1, "ViewBuilds", "View builds")
	_ = tx.AddAssignmentExtended(ctx, "p1", "u-1", 1, 0, 0, 0, 1, 0)
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := st.CompleteRun(ctx, runID, tx.Counts()); err != nil {
		t.Fatalf("complete run: %v", err)
	}
	return st
}

func TestResourceExportEndpointDownloadsTokenScopedCSV(t *testing.T) {
	handler := apiRoutes(exportTestStore(t))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/explorer/resources/export?token=p1", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != "attachment; filename=resource-permissions.csv" {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "resource_token") || !strings.Contains(body, "p1") {
		t.Fatalf("unexpected CSV: %s", body)
	}
}

func TestMatrixExportEndpointDownloadsProjectAndBitScopedCSV(t *testing.T) {
	handler := apiRoutes(exportTestStore(t))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/explorer/matrix/export?projectId=p1&bit=1", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Disposition"); got != "attachment; filename=permission-matrix.csv" {
		t.Fatalf("Content-Disposition = %q", got)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "project_id") || !strings.Contains(body, "permission_bit") || !strings.Contains(body, "ViewBuilds") {
		t.Fatalf("unexpected CSV: %s", body)
	}
}

func TestCollectionStatusStartsIdle(t *testing.T) {
	t.Setenv("AZDO_ORG", "")
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	request := httptest.NewRequest(http.MethodGet, "/api/run/collection-status", nil)
	response := httptest.NewRecorder()
	apiRoutes(st).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", response.Code, response.Body.String())
	}
	var status collect.Status
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.State != collect.StateIdle {
		t.Fatalf("state = %q, want idle", status.State)
	}
}

func TestCollectWithoutOrganizationReturnsActionableSetupError(t *testing.T) {
	t.Setenv("AZDO_ORG", "")
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	request := httptest.NewRequest(http.MethodPost, "/api/run/collect", nil)
	response := httptest.NewRecorder()
	apiRoutes(st).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, body = %s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); !strings.Contains(body, "AZDO_ORG=my-org") || !strings.Contains(body, "restart") {
		t.Fatalf("error is not actionable: %s", body)
	}
}
