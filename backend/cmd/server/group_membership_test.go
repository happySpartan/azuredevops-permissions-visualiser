package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/store"
)

func groupMembershipTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	runID, _ := st.BeginRun(ctx, "acme")
	tx, _ := st.BeginTx(ctx, runID)
	_ = tx.AddSubject(ctx, "g-root", "Engineering", "aad", "group")
	_ = tx.AddSubject(ctx, "u-one", "One", "aad", "user")
	_ = tx.AddMembership(ctx, "g-root", "u-one")
	_ = tx.Commit()
	_ = st.CompleteRun(ctx, runID, tx.Counts())
	return st
}

func TestGroupMembershipAPI(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/explorer/groups/memberships?descriptor=g-root", nil)
	response := httptest.NewRecorder()
	apiRoutes(groupMembershipTestStore(t)).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body store.GroupMembershipDetail
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Group.Descriptor != "g-root" || len(body.Members) != 1 || body.Members[0].Subject.Descriptor != "u-one" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestGroupMembershipAPIRequiresDescriptor(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/explorer/groups/memberships", nil)
	response := httptest.NewRecorder()
	apiRoutes(groupMembershipTestStore(t)).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", response.Code, response.Body.String())
	}
}

func TestGroupMembershipExportAPI(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/explorer/groups/memberships/export?descriptor=g-root", nil)
	response := httptest.NewRecorder()
	apiRoutes(groupMembershipTestStore(t)).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/csv; charset=utf-8" {
		t.Fatalf("content type = %q", contentType)
	}
	if disposition := response.Header().Get("Content-Disposition"); !strings.Contains(disposition, "group-membership.csv") {
		t.Fatalf("content disposition = %q", disposition)
	}
	if body := response.Body.String(); !strings.Contains(body, "g-root,Engineering,u-one,One,user,direct") {
		t.Fatalf("unexpected CSV: %s", body)
	}
}
