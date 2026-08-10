package azdo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// staticToken is a TokenProvider that always returns a fixed token.
type staticToken struct{ t string }

func (s staticToken) Token(context.Context) (string, error) { return s.t, nil }

// fakeOrg spins up an httptest server and a Client pointed at it with a static
// token and no retries. handler receives the request and returns status + body.
func fakeOrg(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c, err := NewClient("myorg",
		WithHTTPClient(srv.Client()),
		WithBaseURL(srv.URL),
		WithTokenProvider(staticToken{t: "tok-123"}),
		WithRetry(0, 0, 0),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, srv
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func TestProjects(t *testing.T) {
	c, srv := fakeOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/myorg/_apis/projects" {
			t.Errorf("path = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-123" {
			t.Errorf("auth header = %q", got)
		}
		writeJSON(w, map[string]any{
			"value": []Project{
				{ID: "p1", Name: "Alpha", State: "wellFormed"},
				{ID: "p2", Name: "Beta", State: "wellFormed"},
			},
		})
	})
	defer srv.Close()

	got, err := c.Projects(context.Background())
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(got) != 2 || got[0].Name != "Alpha" {
		t.Fatalf("unexpected projects: %+v", got)
	}
}

func TestBuildDefinitionsFiltersYAML(t *testing.T) {
	c, srv := fakeOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/myorg/proj/_apis/build/definitions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"value": []BuildDefinition{
				{ID: 1, Name: "ci", Process: struct {
					Type int `json:"type"`
				}{Type: ProcessTypeYAML}},
				{ID: 2, Name: "classic", Process: struct {
					Type int `json:"type"`
				}{Type: ProcessTypeDesigner}},
			},
		})
	})
	defer srv.Close()

	got, err := c.BuildDefinitions(context.Background(), "proj", false)
	if err != nil {
		t.Fatalf("BuildDefinitions: %v", err)
	}
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("expected only YAML def, got %+v", got)
	}

	all, err := c.BuildDefinitions(context.Background(), "proj", true)
	if err != nil {
		t.Fatalf("BuildDefinitions(all): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 with includeNonYAML, got %d", len(all))
	}
}

func TestBuildFolders(t *testing.T) {
	c, srv := fakeOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/myorg/proj/_apis/build/folders" {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"value": []BuildFolder{{Path: "/Shared"}},
		})
	})
	defer srv.Close()

	got, err := c.BuildFolders(context.Background(), "proj")
	if err != nil {
		t.Fatalf("BuildFolders: %v", err)
	}
	if len(got) != 1 || got[0].Path != "/Shared" {
		t.Fatalf("unexpected folders: %+v", got)
	}
}

func TestACEQuery(t *testing.T) {
	c, srv := fakeOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/myorg/_apis/accesscontrollists/"+BuildNamespaceID {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Query().Get("token") != "proj/12" || r.URL.Query().Get("includeExtendedInfo") != "true" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		writeJSON(w, []ACL{{
			Token: "proj/12",
			Entries: map[string]ACLCE{"d-1": {
				Descriptor: "d-1", Allow: 2,
				ExtendedInfo: ACLExtendedInformation{EffectiveAllow: 2},
			}},
		}})
	})
	defer srv.Close()

	got, err := c.ACEQuery(context.Background(), BuildNamespaceID, []string{"proj/12"}, false)
	if err != nil {
		t.Fatalf("ACEQuery: %v", err)
	}
	acl, ok := got["proj/12"]
	if !ok || len(acl.Entries) != 1 || acl.Entries["d-1"].ExtendedInfo.EffectiveAllow != 2 {
		t.Fatalf("unexpected ACL: %+v", acl)
	}
}

func TestACEQueryRequestsEachToken(t *testing.T) {
	requests := 0
	c, srv := fakeOrg(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		writeJSON(w, []ACL{{Token: r.URL.Query().Get("token"), Entries: map[string]ACLCE{}}})
	})
	defer srv.Close()

	if _, err := c.ACEQuery(context.Background(), BuildNamespaceID, []string{"one", "two", "three"}, false); err != nil {
		t.Fatalf("ACEQuery: %v", err)
	}
	if requests != 3 {
		t.Fatalf("expected 3 requests, got %d", requests)
	}
}

func TestSecurityNamespace(t *testing.T) {
	c, srv := fakeOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/myorg/_apis/securitynamespaces/"+BuildNamespaceID {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"namespaceId": BuildNamespaceID,
			"name":        "Build",
			"actions": []ACE{
				{Bit: 1, Name: "ViewBuilds"},
				{Bit: 16384, Name: "AdministerBuildPermissions"},
			},
		})
	})
	defer srv.Close()

	ns, err := c.SecurityNamespace(context.Background(), BuildNamespaceID)
	if err != nil {
		t.Fatalf("SecurityNamespace: %v", err)
	}
	if ns.Name != "Build" || len(ns.Actions) != 2 {
		t.Fatalf("unexpected namespace: %+v", ns)
	}
}

func TestErrorDecoding(t *testing.T) {
	c, srv := fakeOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		writeJSON(w, map[string]any{"message": "Access denied"})
	})
	defer srv.Close()

	_, err := c.Projects(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "azdo: HTTP 403: Access denied" {
		t.Fatalf("unexpected error: %v", err)
	}
}
