package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imonirulislam/plumbline/internal/core"
	"github.com/imonirulislam/plumbline/internal/provider"
)

// newServer mimics a GitLab instance where "acme" is NOT a group (404) but IS a
// user with one non-fork project "acme/web" that has a protected default
// branch, a .gitlab-ci.yml, and a renovate config. A single path handler is
// used (not ServeMux) so encoded "%2F" project ids don't get mis-routed.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/groups/acme/projects"):
			http.Error(w, "not found", http.StatusNotFound) // force user fallback
		case strings.HasSuffix(p, "/users/acme/projects"):
			_, _ = w.Write([]byte(`[
				{"path":"web","default_branch":"main","visibility":"public","archived":false,
				 "web_url":"https://gitlab.com/acme/web","namespace":{"full_path":"acme"}},
				{"path":"forked","default_branch":"main","visibility":"public",
				 "namespace":{"full_path":"acme"},"forked_from_project":{"id":1}}
			]`))
		case strings.Contains(p, "/repository/branches/main"):
			_, _ = w.Write([]byte(`{"name":"main","protected":true}`))
		case strings.HasSuffix(p, "/repository/files/.gitlab-ci.yml"):
			_, _ = w.Write([]byte(`{"file_name":".gitlab-ci.yml"}`))
		case strings.HasSuffix(p, "/repository/files/renovate.json"):
			_, _ = w.Write([]byte(`{"file_name":"renovate.json"}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestListReposFallsBackToUserAndFiltersForks(t *testing.T) {
	srv := newServer(t)
	c, err := provider.Open("gitlab", provider.Config{BaseURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	repos, err := c.ListRepos(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("got %d repos, want 1 (fork filtered)", len(repos))
	}
	if repos[0].Slug() != "acme/web" || repos[0].DefaultBranch != "main" {
		t.Fatalf("unexpected repo: %+v", repos[0])
	}
	if repos[0].Private {
		t.Errorf("public project reported as private")
	}
}

func TestInspectNormalizesFacts(t *testing.T) {
	srv := newServer(t)
	c, err := provider.Open("gitlab", provider.Config{BaseURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	ref := core.RepoRef{Owner: "acme", Name: "web", DefaultBranch: "main"}
	st, err := c.Inspect(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if st.DefaultBranchProtected != core.TriYes {
		t.Errorf("protected: got %v, want yes", st.DefaultBranchProtected)
	}
	if st.HasCI != core.TriYes {
		t.Errorf("ci: got %v, want yes", st.HasCI)
	}
	if st.HasDependencyAutomation != core.TriYes {
		t.Errorf("dependency-automation: got %v, want yes", st.HasDependencyAutomation)
	}
}

func TestTokenRequired(t *testing.T) {
	if _, err := provider.Open("gitlab", provider.Config{}); err == nil {
		t.Fatal("expected error when token is missing")
	}
}
