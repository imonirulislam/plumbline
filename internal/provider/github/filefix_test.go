package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imonirulislam/plumbline/internal/core"
	"github.com/imonirulislam/plumbline/internal/provider"
)

// fileFixServer mocks the GitHub endpoints openRenovatePR walks. existingPR
// controls whether an open PR is already present (idempotency path).
func fileFixServer(t *testing.T, existingPR bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(p, "/pulls"):
			if existingPR {
				_, _ = w.Write([]byte(`[{"html_url":"https://github.com/acme/web/pull/1"}]`))
			} else {
				_, _ = w.Write([]byte(`[]`))
			}
		case r.Method == http.MethodGet && strings.Contains(p, "/git/ref/heads/"):
			_, _ = w.Write([]byte(`{"object":{"sha":"deadbeef"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/git/refs"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPut && strings.Contains(p, "/contents/"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/pulls"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"html_url":"https://github.com/acme/web/pull/2"}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+p, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newClient(t *testing.T, base string) provider.FileRemediator {
	t.Helper()
	p, err := provider.Open("github", provider.Config{Token: "t", BaseURL: base})
	if err != nil {
		t.Fatal(err)
	}
	fr, ok := p.(provider.FileRemediator)
	if !ok {
		t.Fatal("github client does not implement FileRemediator")
	}
	return fr
}

func TestOpenRenovatePR_Creates(t *testing.T) {
	srv := fileFixServer(t, false)
	fr := newClient(t, srv.URL)
	ref := core.RepoRef{Owner: "acme", Name: "web", DefaultBranch: "main"}
	url, err := fr.OpenFix(context.Background(), ref, "dependency-automation")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/acme/web/pull/2" {
		t.Fatalf("PR url = %q, want .../pull/2", url)
	}
}

func TestOpenRenovatePR_Idempotent(t *testing.T) {
	srv := fileFixServer(t, true) // a PR already exists
	fr := newClient(t, srv.URL)
	ref := core.RepoRef{Owner: "acme", Name: "web", DefaultBranch: "main"}
	url, err := fr.OpenFix(context.Background(), ref, "dependency-automation")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/acme/web/pull/1" {
		t.Fatalf("expected the existing PR .../pull/1, got %q", url)
	}
}
