package collect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/azdo"
	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/store"
)

type recordedSnapshot struct {
	Projects        json.RawMessage            `json:"projects"`
	Folders         json.RawMessage            `json:"folders"`
	DefinitionPages []json.RawMessage          `json:"definitionPages"`
	UserPages       []json.RawMessage          `json:"userPages"`
	Groups          json.RawMessage            `json:"groups"`
	Memberships     json.RawMessage            `json:"memberships"`
	Namespace       json.RawMessage            `json:"namespace"`
	ACLByToken      map[string]json.RawMessage `json:"aclByToken"`
}

func TestRecordedSnapshotCollectionCommitsQueryableAnalysis(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "azure_devops_snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot recordedSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	serve := func(body json.RawMessage) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		}
	}
	mux.HandleFunc("/org/_apis/projects", serve(snapshot.Projects))
	mux.HandleFunc("/org/Alpha/_apis/build/folders", serve(snapshot.Folders))
	mux.HandleFunc("/org/Alpha/_apis/build/definitions", func(w http.ResponseWriter, r *http.Request) {
		page := 0
		if r.URL.Query().Get("continuationToken") == "defs-page-2" {
			page = 1
		}
		serve(snapshot.DefinitionPages[page])(w, r)
	})
	mux.HandleFunc("/org/_apis/graph/users", func(w http.ResponseWriter, r *http.Request) {
		page := 0
		if r.URL.Query().Get("continuationToken") == "users-page-2" {
			page = 1
		}
		serve(snapshot.UserPages[page])(w, r)
	})
	mux.HandleFunc("/org/_apis/graph/groups", serve(snapshot.Groups))
	mux.HandleFunc("/org/_apis/graph/memberships/", serve(snapshot.Memberships))
	mux.HandleFunc("/org/_apis/Identities", func(w http.ResponseWriter, r *http.Request) {
		descriptors := r.URL.Query().Get("descriptors")
		values := []map[string]any{}
		if descriptors != "" {
			for _, d := range strings.Split(descriptors, ",") {
				values = append(values, map[string]any{"descriptor": d, "subjectDescriptor": d})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"value": values})
	})
	mux.HandleFunc("/org/_apis/securitynamespaces/", serve(snapshot.Namespace))
	mux.HandleFunc("/org/_apis/accesscontrollists/"+azdo.BuildNamespaceID, func(w http.ResponseWriter, r *http.Request) {
		serve(snapshot.ACLByToken[r.URL.Query().Get("token")])(w, r)
	})
	mux.HandleFunc("/org/Alpha/_apis/git/repositories", func(w http.ResponseWriter, r *http.Request) {
		serve(json.RawMessage(`{"value":[]}`))(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := azdo.NewClient("org", azdo.WithHTTPClient(srv.Client()), azdo.WithBaseURL(srv.URL), azdo.WithVSSPSURL(srv.URL), azdo.WithTokenProvider(staticTok{}), azdo.WithRetry(0, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "snapshot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	result, err := New(client, st).Collect(context.Background(), "org")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if result.Counts != (store.RunCounts{Projects: 1, Folders: 1, Pipelines: 2, Subjects: 3, Assignments: 4}) {
		t.Fatalf("counts = %+v", result.Counts)
	}
	run, err := st.RunByID(context.Background(), result.RunID)
	if err != nil || run.Status != store.StatusComplete {
		t.Fatalf("committed run = %+v, err = %v", run, err)
	}
	resources, err := st.ResourcesByRun(context.Background(), result.RunID)
	if err != nil || len(resources) != 1 || len(resources[0].Pipelines) != 2 || resources[0].Pipelines[1].Name != "Deploy" {
		t.Fatalf("resources = %+v, err = %v", resources, err)
	}
	subjects, err := st.SubjectsByRun(context.Background(), result.RunID, store.SubjectQuery{Limit: 10})
	if err != nil || subjects.Total != 3 {
		t.Fatalf("subjects = %+v, err = %v", subjects, err)
	}
	permissions, err := st.ResourcePermissionsByRun(context.Background(), result.RunID, "PROJ-A/Shared/12")
	if err != nil || len(permissions.Subjects) != 1 || permissions.Subjects[0].Subject.DisplayName != "Bob" || permissions.Subjects[0].Permissions[1].State != store.PermissionDeny {
		t.Fatalf("permissions = %+v, err = %v", permissions, err)
	}
}
