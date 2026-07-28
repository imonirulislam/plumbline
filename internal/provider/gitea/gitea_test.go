package gitea

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/imonirulislam/plumbline/internal/core"
	"github.com/imonirulislam/plumbline/internal/provider"
)

// newServer stands in for a Gitea instance with one non-fork repo "acme/web"
// that has a protected default branch, Gitea Actions, and a renovate config.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/users/acme/repos", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"name":"web","private":false,"archived":false,"fork":false,
			 "default_branch":"main","html_url":"https://gitea.example/acme/web",
			 "owner":{"login":"acme"}},
			{"name":"mirror","fork":true,"default_branch":"main","owner":{"login":"acme"}}
		]`))
	})
	mux.HandleFunc("/api/v1/repos/acme/web/branches/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"main","protected":true}`))
	})
	mux.HandleFunc("/api/v1/repos/acme/web/contents/.gitea/workflows", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"ci.yml","type":"file"}]`)) // directory listing
	})
	mux.HandleFunc("/api/v1/repos/acme/web/contents/renovate.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"renovate.json","type":"file"}`))
	})
	// Everything else 404s (Gitea's contents API for a missing path).
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestListReposFiltersForks(t *testing.T) {
	srv := newServer(t)
	c, err := provider.Open("gitea", provider.Config{BaseURL: srv.URL, Token: "t"})
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
}

func TestInspectNormalizesFacts(t *testing.T) {
	srv := newServer(t)
	c, err := provider.Open("gitea", provider.Config{BaseURL: srv.URL, Token: "t"})
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

func TestBaseURLRequired(t *testing.T) {
	if _, err := provider.Open("gitea", provider.Config{Token: "t"}); err == nil {
		t.Fatal("expected error when base URL is missing")
	}
}
