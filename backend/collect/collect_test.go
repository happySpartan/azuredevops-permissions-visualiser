package collect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/azdo"
	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/store"
)

// staticTok is a no-op token provider for tests.
type staticTok struct{}

func (staticTok) Token(context.Context) (string, error) { return "tok", nil }

// fakeServer mimics the subset of the Azure DevOps API the collector uses.
type fakeServer struct {
	projectErr       bool // fail the projects phase
	foldersErr       bool
	defsErr          bool
	reposErr         bool
	branchesErr      bool
	poolsErr         bool
	endpointsErr     bool
	variableGroupsErr bool
	usersErr         bool
	groupsErr        bool
	membersErr       bool
	aclErr           bool
	gitACLErr        bool
	resourceACLErr   bool
	aclOnlyGroup     bool // ACL identity is omitted by the Graph groups listing
	projectCalls     int
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

	mux.HandleFunc("/org/Alpha/_apis/git/repositories", func(w http.ResponseWriter, r *http.Request) {
		if f.reposErr {
			http.Error(w, `{"message":"repos denied"}`, http.StatusForbidden)
			return
		}
		writeJSON(w, map[string]any{"value": []azdo.GitRepository{
			{ID: "REPO-1", Name: "mainrepo", DefaultBranch: "refs/heads/main", Project: struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{ID: "PROJ-A", Name: "Alpha"}},
		}})
	})

	mux.HandleFunc("/org/Alpha/_apis/git/repositories/REPO-1/refs", func(w http.ResponseWriter, r *http.Request) {
		if f.branchesErr {
			http.Error(w, `{"message":"branches denied"}`, http.StatusForbidden)
			return
		}
		writeJSON(w, map[string]any{"value": []azdo.GitBranch{
			{Name: "refs/heads/main"},
			{Name: "refs/heads/develop"},
		}})
	})

	mux.HandleFunc("/org/_apis/distributedtask/pools", func(w http.ResponseWriter, r *http.Request) {
		if f.poolsErr {
			http.Error(w, `{"message":"pools denied"}`, http.StatusForbidden)
			return
		}
		writeJSON(w, map[string]any{"value": []azdo.AgentPool{
			{ID: 1, Name: "Azure Pipelines", IsHosted: true},
			{ID: 7, Name: "Self-hosted"},
		}})
	})

	mux.HandleFunc("/org/Alpha/_apis/serviceendpoint/endpoints", func(w http.ResponseWriter, r *http.Request) {
		if f.endpointsErr {
			http.Error(w, `{"message":"endpoints denied"}`, http.StatusForbidden)
			return
		}
		writeJSON(w, map[string]any{"value": []azdo.ServiceEndpoint{
			{ID: "EP-1", Name: "npm registry", Type: "npm"},
		}})
	})

	mux.HandleFunc("/org/Alpha/_apis/distributedtask/variablegroups", func(w http.ResponseWriter, r *http.Request) {
		if f.variableGroupsErr {
			http.Error(w, `{"message":"variable groups denied"}`, http.StatusForbidden)
			return
		}
		writeJSON(w, map[string]any{"value": []azdo.VariableGroup{
			{ID: 3, Name: "shared-secrets"},
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
		nsID := strings.TrimPrefix(r.URL.Path, "/org/_apis/securitynamespaces/")
		switch nsID {
		case azdo.BuildNamespaceID:
			writeJSON(w, map[string]any{"value": []map[string]any{{
				"namespaceId": azdo.BuildNamespaceID,
				"name":        "Build",
				"actions":     []azdo.ACE{{Bit: 1, Name: "ViewBuilds"}},
			}}})
		case azdo.GitNamespaceID:
			writeJSON(w, map[string]any{"value": []map[string]any{{
				"namespaceId": azdo.GitNamespaceID,
				"name":        "Git Repositories",
				"actions":     []azdo.ACE{{Bit: 1, Name: "Administer"}, {Bit: 2, Name: "GenericRead"}},
			}}})
		case azdo.BuildAdministrationNamespaceID:
			writeJSON(w, map[string]any{"value": []map[string]any{{
				"namespaceId": azdo.BuildAdministrationNamespaceID,
				"name":        "BuildAdministration",
				"actions":     []azdo.ACE{{Bit: 1, Name: "ViewBuildResources"}, {Bit: 4, Name: "UseBuildResources"}},
			}}})
		case azdo.ServiceEndpointsNamespaceID:
			writeJSON(w, map[string]any{"value": []map[string]any{{
				"namespaceId": azdo.ServiceEndpointsNamespaceID,
				"name":        "ServiceEndpoints",
				"actions":     []azdo.ACE{{Bit: 1, Name: "Use"}, {Bit: 16, Name: "ViewEndpoint"}},
			}}})
		case azdo.LibraryNamespaceID:
			writeJSON(w, map[string]any{"value": []map[string]any{{
				"namespaceId": azdo.LibraryNamespaceID,
				"name":        "Library",
				"actions":     []azdo.ACE{{Bit: 1, Name: "View"}, {Bit: 16, Name: "Use"}},
			}}})
		default:
			http.Error(w, `{"message":"unknown namespace"}`, http.StatusNotFound)
		}
	})

	// The identity API echoes each general descriptor's Graph (subject)
	// descriptor. The fake ACLs already use Graph descriptors, so map each
	// descriptor to itself to preserve the subject join.
	mux.HandleFunc("/org/_apis/Identities", func(w http.ResponseWriter, r *http.Request) {
		descriptors := r.URL.Query().Get("descriptors")
		values := []map[string]any{}
		if descriptors != "" {
			for _, d := range strings.Split(descriptors, ",") {
				identity := map[string]any{"descriptor": d, "subjectDescriptor": d}
				if d == "Microsoft.TeamFoundation.Identity;acl-only" {
					identity["subjectDescriptor"] = "vssgp.acl-only"
					identity["providerDisplayName"] = "[org]\\Project Collection Administrators"
					identity["isContainer"] = true
				}
				values = append(values, identity)
			}
		}
		writeJSON(w, map[string]any{"value": values})
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
			descriptor := "g-1"
			if f.aclOnlyGroup {
				descriptor = "Microsoft.TeamFoundation.Identity;acl-only"
			}
			entries[descriptor] = azdo.ACLCE{Descriptor: descriptor, Allow: 3, ExtendedInfo: azdo.ACLExtendedInformation{EffectiveAllow: 3}}
		case "PROJ-A/Shared":
			entries["u-1"] = azdo.ACLCE{Descriptor: "u-1", Allow: 1, ExtendedInfo: azdo.ACLExtendedInformation{EffectiveAllow: 1}}
		case "PROJ-A/Shared/12":
			entries["g-1"] = azdo.ACLCE{Descriptor: "g-1", Allow: 2, ExtendedInfo: azdo.ACLExtendedInformation{EffectiveAllow: 2}}
		}
		writeJSON(w, map[string]any{"value": []azdo.ACL{{Token: token, Entries: entries}}})
	})

	mux.HandleFunc("/org/_apis/accesscontrollists/"+azdo.GitNamespaceID, func(w http.ResponseWriter, r *http.Request) {
		if f.gitACLErr {
			http.Error(w, `{"message":"git acl denied"}`, http.StatusForbidden)
			return
		}
		token := r.URL.Query().Get("token")
		entries := map[string]azdo.ACLCE{}
		switch token {
		case "REPO-1":
			entries["u-1"] = azdo.ACLCE{Descriptor: "u-1", Allow: 3, ExtendedInfo: azdo.ACLExtendedInformation{EffectiveAllow: 3}}
		case "REPO-1/refs/heads/main":
			entries["g-1"] = azdo.ACLCE{Descriptor: "g-1", Allow: 2, ExtendedInfo: azdo.ACLExtendedInformation{EffectiveAllow: 2}}
		}
		writeJSON(w, map[string]any{"value": []azdo.ACL{{Token: token, Entries: entries}}})
	})

	mux.HandleFunc("/org/_apis/accesscontrollists/"+azdo.BuildAdministrationNamespaceID, func(w http.ResponseWriter, r *http.Request) {
		if f.resourceACLErr {
			http.Error(w, `{"message":"resource acl denied"}`, http.StatusForbidden)
			return
		}
		token := r.URL.Query().Get("token")
		entries := map[string]azdo.ACLCE{}
		switch token {
		case "pools/1":
			entries["u-1"] = azdo.ACLCE{Descriptor: "u-1", Allow: 5, ExtendedInfo: azdo.ACLExtendedInformation{EffectiveAllow: 5}}
		}
		writeJSON(w, map[string]any{"value": []azdo.ACL{{Token: token, Entries: entries}}})
	})

	mux.HandleFunc("/org/_apis/accesscontrollists/"+azdo.ServiceEndpointsNamespaceID, func(w http.ResponseWriter, r *http.Request) {
		if f.resourceACLErr {
			http.Error(w, `{"message":"resource acl denied"}`, http.StatusForbidden)
			return
		}
		token := r.URL.Query().Get("token")
		entries := map[string]azdo.ACLCE{}
		switch token {
		case "PROJ-A/EP-1":
			entries["g-1"] = azdo.ACLCE{Descriptor: "g-1", Allow: 17, ExtendedInfo: azdo.ACLExtendedInformation{EffectiveAllow: 17}}
		}
		writeJSON(w, map[string]any{"value": []azdo.ACL{{Token: token, Entries: entries}}})
	})

	mux.HandleFunc("/org/_apis/accesscontrollists/"+azdo.LibraryNamespaceID, func(w http.ResponseWriter, r *http.Request) {
		if f.resourceACLErr {
			http.Error(w, `{"message":"resource acl denied"}`, http.StatusForbidden)
			return
		}
		token := r.URL.Query().Get("token")
		entries := map[string]azdo.ACLCE{}
		switch token {
		case "PROJ-A/3":
			entries["u-1"] = azdo.ACLCE{Descriptor: "u-1", Allow: 17, ExtendedInfo: azdo.ACLExtendedInformation{EffectiveAllow: 17}}
		}
		writeJSON(w, map[string]any{"value": []azdo.ACL{{Token: token, Entries: entries}}})
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
		azdo.WithVSSPSURL(srv.URL),
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
	want := []Phase{PhaseProjects, PhaseBuilds, PhaseRepositories, PhaseResources, PhaseSubjects, PhasePermissions, PhaseCommitting, PhaseComplete}
	if len(phases) != len(want) {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
	for i := range want {
		if phases[i] != want[i] {
			t.Fatalf("phases[%d] = %q, want %q (all phases: %v)", i, phases[i], want[i], phases)
		}
	}
}

func TestCancelledCollectionMarksRunCancelledAndDiscardsPartialData(t *testing.T) {
	f := &fakeServer{}
	c, st, srv := newCollector(t, f)
	defer srv.Close()
	runID, err := st.BeginRun(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := st.BeginTx(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.AddProject(context.Background(), "p1", "Alpha"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := c.fail(context.Background(), runID, store.StatusFailed, context.Canceled); err != nil {
		t.Fatal(err)
	}

	var status string
	var projects int
	if err := st.DB().QueryRow(`SELECT status FROM runs WHERE id=?`, runID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&projects); err != nil {
		t.Fatal(err)
	}
	if status != string(store.StatusCancelled) || projects != 0 {
		t.Fatalf("status=%q projects=%d, want cancelled and no partial data", status, projects)
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
	if res.Counts.Repositories != 1 || res.Counts.Branches != 2 {
		t.Fatalf("repository counts = %+v, want 1 repo and 2 branches", res.Counts)
	}
	if res.Counts.AgentPools != 2 || res.Counts.Endpoints != 1 || res.Counts.VariableGroups != 1 {
		t.Fatalf("pipeline resource counts = %+v, want 2 pools, 1 endpoint, 1 vg", res.Counts)
	}
	if res.Counts.Subjects != 2 { // 1 user + 1 group
		t.Fatalf("subject count = %d, want 2", res.Counts.Subjects)
	}
	// Build: 3 assignments. Git: 2 (REPO-1 + branch). Pipeline resources: 3 (pools/1, EP-1, PROJ-A/3). Total 8.
	if res.Counts.Assignments != 8 {
		t.Fatalf("assignment count = %d, want 8", res.Counts.Assignments)
	}

	run, err := st.RunByID(context.Background(), res.RunID)
	if err != nil {
		t.Fatalf("RunByID: %v", err)
	}
	if run.Status != store.StatusComplete {
		t.Fatalf("status = %s, want complete", run.Status)
	}
}

func TestCollectAddsACLIdentitiesOmittedFromGraphListings(t *testing.T) {
	f := &fakeServer{aclOnlyGroup: true}
	c, st, srv := newCollector(t, f)
	defer srv.Close()

	res, err := c.Collect(context.Background(), "org")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if res.Counts.Subjects != 3 {
		t.Fatalf("subject count = %d, want Graph subjects plus ACL-only identity", res.Counts.Subjects)
	}

	var name, kind string
	err = st.DB().QueryRow(`SELECT display_name, subject_kind FROM subjects WHERE run_id=? AND descriptor=?`,
		res.RunID, "vssgp.acl-only").Scan(&name, &kind)
	if err != nil {
		t.Fatalf("ACL-only subject not stored: %v", err)
	}
	if name != "[org]\\Project Collection Administrators" || kind != "group" {
		t.Fatalf("ACL-only subject = (%q, %q), want resolved group metadata", name, kind)
	}

	var unresolved int
	err = st.DB().QueryRow(`
		SELECT COUNT(*)
		FROM assignments a
		LEFT JOIN subjects s ON s.run_id=a.run_id AND s.descriptor=a.descriptor
		WHERE a.run_id=? AND s.descriptor IS NULL`, res.RunID).Scan(&unresolved)
	if err != nil {
		t.Fatal(err)
	}
	if unresolved != 0 {
		t.Fatalf("unresolved assignment subjects = %d, want 0", unresolved)
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
