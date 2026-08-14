package azdo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
		WithVSSPSURL(srv.URL),
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

func TestMSAPassThroughHeader(t *testing.T) {
	assertHeader := func(want string) {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("X-VSS-ForceMsaPassThrough"); got != want {
				t.Errorf("X-VSS-ForceMsaPassThrough = %q, want %q", got, want)
			}
			writeJSON(w, map[string]any{"value": []Project{}})
		}))
		defer srv.Close()
		c, err := NewClient("myorg",
			WithHTTPClient(srv.Client()),
			WithBaseURL(srv.URL),
			WithVSSPSURL(srv.URL),
			WithTokenProvider(staticToken{t: "tok-123"}),
			WithRetry(0, 0, 0),
			WithForceMsaPassThrough(want == "true"),
		)
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if _, err := c.Projects(context.Background()); err != nil {
			t.Fatalf("Projects: %v", err)
		}
	}
	// Defaults are covered by TestProjects; here assert both explicit settings.
	assertHeader("true")  // personal MSA account
	assertHeader("")      // work/school (Entra) account / managed identity
}

func TestProjects(t *testing.T) {
	c, srv := fakeOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/myorg/_apis/projects" {
			t.Errorf("path = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-123" {
			t.Errorf("auth header = %q", got)
		}
		// Azure DevOps requires the MSA pass-through header for personal
		// Microsoft accounts; without it a valid token is redirected to sign-in.
		if got := r.Header.Get("X-VSS-ForceMsaPassThrough"); got != "true" {
			t.Errorf("msa pass-through header = %q", got)
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
		writeJSON(w, map[string]any{"value": []ACL{{
			Token: "proj/12",
			Entries: map[string]ACLCE{"d-1": {
				Descriptor: "d-1", Allow: 2,
				ExtendedInfo: ACLExtendedInformation{EffectiveAllow: 2},
			}},
		}}})
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
		writeJSON(w, map[string]any{"value": []ACL{{Token: r.URL.Query().Get("token"), Entries: map[string]ACLCE{}}}})
	})
	defer srv.Close()

	if _, err := c.ACEQuery(context.Background(), BuildNamespaceID, []string{"one", "two", "three"}, false); err != nil {
		t.Fatalf("ACEQuery: %v", err)
	}
	if requests != 3 {
		t.Fatalf("expected 3 requests, got %d", requests)
	}
}

func TestIdentityGraphDescriptors(t *testing.T) {
	c, srv := fakeOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/myorg/_apis/Identities" {
			t.Errorf("path = %q", r.URL.Path)
		}
		got := r.URL.Query().Get("descriptors")
		// already-graph descriptors should be filtered client-side
		if strings.Contains(got, "vssgp.") {
			t.Errorf("graph descriptors leaked into identity call: %q", got)
		}
		if !strings.Contains(got, "Microsoft.TeamFoundation.Identity;S-1-9-1") ||
			!strings.Contains(got, "Microsoft.TeamFoundation.Identity;S-1-9-2") {
			t.Errorf("expected general descriptors in call, got %q", got)
		}
		writeJSON(w, map[string]any{"value": []map[string]string{
			// Azure DevOps may canonicalize the returned storage descriptor, so
			// it is not necessarily byte-for-byte equal to the requested one.
			{"descriptor": "Microsoft.TeamFoundation.Identity;canonical-1", "subjectDescriptor": "vssgp.abc"},
			{"descriptor": "Microsoft.TeamFoundation.Identity;S-1-9-2", "subjectDescriptor": "vssgp.def"},
		}})
	})
	defer srv.Close()

	got, err := c.IdentityGraphDescriptors(context.Background(), []string{
		"Microsoft.TeamFoundation.Identity;S-1-9-1",
		"Microsoft.TeamFoundation.Identity;S-1-9-2",
		"vssgp.alreadyGraph", // must be ignored
		"",
	})
	if err != nil {
		t.Fatalf("IdentityGraphDescriptors: %v", err)
	}
	if got["Microsoft.TeamFoundation.Identity;S-1-9-1"].Descriptor != "vssgp.abc" ||
		got["Microsoft.TeamFoundation.Identity;S-1-9-2"].Descriptor != "vssgp.def" {
		t.Fatalf("unexpected mapping: %+v", got)
	}
	if _, ok := got["vssgp.alreadyGraph"]; ok {
		t.Fatalf("graph descriptor should not be resolved: %+v", got)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 resolved descriptors, got %d", len(got))
	}
}

func TestSecurityNamespace(t *testing.T) {
	c, srv := fakeOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/myorg/_apis/securitynamespaces/"+BuildNamespaceID {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeJSON(w, map[string]any{"value": []map[string]any{{
			"namespaceId": BuildNamespaceID,
			"name":        "Build",
			"actions": []ACE{
				{Bit: 1, Name: "ViewBuilds"},
				{Bit: 16384, Name: "AdministerBuildPermissions"},
			},
		}}})
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

func TestProjectsRedirectToSignIn(t *testing.T) {
	// Azure DevOps redirects to its sign-in page when the caller's bearer
	// token is not accepted. When the client does not auto-follow redirects,
	// it must surface a clear, actionable error rather than decoding the HTML.
	c, srv := fakeOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://spsprodneu1.vssps.visualstudio.com/_signin?realm=dev.azure.com")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusFound)
		_, _ = w.Write([]byte("<html><head><title>Object moved</title></head><body></body></html>"))
	})
	defer srv.Close()
	// Do not let the test HTTP client follow the redirect, so the response
	// returns to get() as a 3xx for the redirect branch to handle.
	srv.Client().CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	_, err := c.Projects(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	want := "azdo: HTTP 302: authentication required (Azure DevOps redirected to its sign-in page);"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("error leaks HTML decode failure instead of actionable message: %v", err)
	}
}

func TestHTMLPageInsteadOfJSON(t *testing.T) {
	// A 200 with an HTML body (e.g. a captive sign-in/error page) must be
	// reported as an auth problem, not left to json.Decode to fail on.
	c, srv := fakeOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>Please sign in</body></html>"))
	})
	defer srv.Close()

	_, err := c.Projects(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "HTML") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTMLErrorPage(t *testing.T) {
	// An HTML authorization-error page (for example Azure DevOps' sign-in page
	// served with a non-2xx status) gets a clear message instead of raw HTML.
	c, srv := fakeOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("<html><body>TF400813: access denied</body></html>"))
	})
	defer srv.Close()

	_, err := c.Projects(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "HTML error page") {
		t.Fatalf("unexpected error: %v", err)
	}
}
