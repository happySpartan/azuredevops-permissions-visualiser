package collect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/azdo"
	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/store"
)

// staticTok is a no-op token provider for tests.
type staticTok struct{}

func (staticTok) Token(context.Context) (string, error) { return "tok", nil }

// fakeServer mimics the subset of the Azure DevOps API the collector uses.
type fakeServer struct {
	projectErr   bool // fail the projects phase
	foldersErr   bool
	defsErr      bool
	usersErr     bool
	groupsErr    bool
	membersErr   bool
	aclErr       bool
	projectCalls int
}

func (f *fakeServer) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/org/_apis/projects", func(w http.ResponseWriter, r *http.Request) {
		f.projectCalls++
		if f.projectErr {
			http.Error(w, `{"message":"projects denied"}`, http.StatusForbidden)
			return
		}
		writeJSON(w, map[string]any{"value": []azdo.Project{
			{ID: "PROJ-A", Name: "Alpha", State: "wellFormed"},
		}})
	})

	mux.HandleFunc("/org/Alpha/_apis/build/folders", func(w http.ResponseWriter, r *http.Request) {
		if f.foldersErr {
			http.Error(w, `{"message":"folders denied"}`, http.StatusForbidden)
			return
		}
		writeJSON(w, map[string]any{"value": []azdo.BuildFolder{{Path: "/Shared"}}})
	})

	mux.HandleFunc("/org/Alpha/_apis/build/definitions", func(w http.ResponseWriter, r *http.Request) {
		if f.defsErr {
			http.Error(w, `{"message":"defs denied"}`, http.StatusForbidden)
			return
		}
		writeJSON(w, map[string]any{"value": []azdo.BuildDefinition{
			{ID: 12, Name: "CI", Path: "/Shared", Process: struct {
				Type int `json:"type"`
			}{Type: azdo.ProcessTypeYAML}},
		}})
	})

	mux.HandleFunc("/org/_apis/graph/users", func(w http.ResponseWriter, r *http.Request) {
		if f.usersErr {
			http.Error(w, `{"message":"users denied"}`, http.StatusForbidden)
			return
		}
		writeJSON(w, map[string]any{"value": []azdo.GraphSubject{
			{Descriptor: "u-1", DisplayName: "Alice", Origin: "aad", SubjectKind: "user"},
		}})
	})

	mux.HandleFunc("/org/_apis/graph/groups", func(w http.ResponseWriter, r *http.Request) {
		if f.groupsErr {
			http.Error(w, `{"message":"groups denied"}`, http.StatusForbidden)
			return
		}
		writeJSON(w, map[string]any{"value": []azdo.GraphSubject{
			{Descriptor: "g-1", DisplayName: "Team", Origin: "vsts", SubjectKind: "group"},
		}})
	})

	mux.HandleFunc("/org/_apis/graph/memberships/", func(w http.ResponseWriter, r *http.Request) {
		if f.membersErr {
			http.Error(w, `{"message":"memberships denied"}`, http.StatusForbidden)
			return
		}
		writeJSON(w, map[string]any{"value": []map[string]string{
			{"membershipId": "u-1"},
		}})
	})

	mux.HandleFunc("/org/_apis/securitynamespaces/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"namespaceId": azdo.BuildNamespaceID,
			"name":        "Build",
			"actions":     []azdo.ACE{{Bit: 1, Name: "ViewBuilds"}},
		})
	})

	mux.HandleFunc("/org/_apis/accesscontrollists/"+azdo.BuildNamespaceID, func(w http.ResponseWriter, r *http.Request) {
		if f.aclErr {
			http.Error(w, `{"message":"acl denied"}`, http.StatusForbidden)
			return
		}
		token := r.URL.Query().Get("token")
		entries := map[string]azdo.ACLCE{}
		switch token {
		case "PROJ-A":
			entries["g-1"] = azdo.ACLCE{Descriptor: "g-1", Allow: 3, ExtendedInfo: azdo.ACLExtendedInformation{EffectiveAllow: 3}}
		case "PROJ-A/Shared":
			entries["u-1"] = azdo.ACLCE{Descriptor: "u-1", Allow: 1, ExtendedInfo: azdo.ACLExtendedInformation{EffectiveAllow: 1}}
		case "PROJ-A/Shared/12":
			entries["g-1"] = azdo.ACLCE{Descriptor: "g-1", Allow: 2, ExtendedInfo: azdo.ACLExtendedInformation{EffectiveAllow: 2}}
		}
		writeJSON(w, []azdo.ACL{{Token: token, Entries: entries}})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func newCollector(t *testing.T, f *fakeServer) (*Collector, *store.Store, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	client, err := azdo.NewClient("org",
		azdo.WithHTTPClient(srv.Client()),
		azdo.WithBaseURL(srv.URL),
		azdo.WithTokenProvider(staticTok{}),
		azdo.WithRetry(0, 0, 0),
	)
	if err != nil {
		t.Fatalf("azdo.NewClient: %v", err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(client, st), st, srv
}

func TestCollectReportsOrderedPhases(t *testing.T) {
	f := &fakeServer{}
	c, _, srv := newCollector(t, f)
	defer srv.Close()

	var phases []Phase
	_, err := c.CollectWithProgress(context.Background(), "org", func(progress Progress) {
		phases = append(phases, progress.Phase)
	})
	if err != nil {
		t.Fatalf("CollectWithProgress: %v", err)
	}
	want := []Phase{PhaseProjects, PhaseBuilds, PhaseSubjects, PhasePermissions, PhaseCommitting, PhaseComplete}
	if len(phases) != len(want) {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
	for i := range want {
		if phases[i] != want[i] {
			t.Fatalf("phases[%d] = %q, want %q (all phases: %v)", i, phases[i], want[i], phases)
		}
	}
}

func TestCollectSuccess(t *testing.T) {
	f := &fakeServer{}
	c, st, srv := newCollector(t, f)
	defer srv.Close()

	res, err := c.Collect(context.Background(), "org")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if res.Counts.Projects != 1 || res.Counts.Folders != 1 || res.Counts.Pipelines != 1 {
		t.Fatalf("unexpected counts: %+v", res.Counts)
	}
	if res.Counts.Subjects != 2 { // 1 user + 1 group
		t.Fatalf("subject count = %d, want 2", res.Counts.Subjects)
	}
	if res.Counts.Assignments != 3 {
		t.Fatalf("assignment count = %d, want 3", res.Counts.Assignments)
	}

	run, err := st.RunByID(context.Background(), res.RunID)
	if err != nil {
		t.Fatalf("RunByID: %v", err)
	}
	if run.Status != store.StatusComplete {
		t.Fatalf("status = %s, want complete", run.Status)
	}
}

func TestCollectFailurePreservesPrevious(t *testing.T) {
	// First a successful run.
	f := &fakeServer{}
	c, st, srv := newCollector(t, f)
	defer srv.Close()
	ctx := context.Background()

	ok, err := c.Collect(ctx, "org")
	if err != nil {
		t.Fatalf("first Collect: %v", err)
	}

	// Now fail the ACL phase.
	f.aclErr = true
	_, err = c.Collect(ctx, "org")
	if err == nil {
		t.Fatal("expected error on failed ACL phase")
	}

	previous, err := st.RunByID(ctx, ok.RunID)
	if err != nil {
		t.Fatalf("previous run should exist: %v", err)
	}
	if previous.Status != store.StatusComplete {
		t.Fatalf("previous status = %s, want complete", previous.Status)
	}

	// The failed run must not have become the latest analysis.
	latest, _ := st.LatestRunID(ctx)
	if latest != ok.RunID {
		t.Fatalf("latest = %d, want original %d", latest, ok.RunID)
	}
}

func TestCollectProjectsPhaseFailure(t *testing.T) {
	f := &fakeServer{projectErr: true}
	c, st, srv := newCollector(t, f)
	defer srv.Close()

	_, err := c.Collect(context.Background(), "org")
	if err == nil {
		t.Fatal("expected error")
	}
	// No completed run (the failed run may leave a failed row, but never a
	// complete one).
	latest, _ := st.LatestRunID(ctx())
	if latest != 0 {
		if r, err2 := st.RunByID(ctx(), latest); err2 == nil && r.Status == store.StatusComplete {
			t.Fatalf("expected no completed run, got latest %d complete", latest)
		}
	}
}

func ctx() context.Context { return context.Background() }
