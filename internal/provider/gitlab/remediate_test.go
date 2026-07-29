package gitlab

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imonirulislam/plumbline/internal/core"
	"github.com/imonirulislam/plumbline/internal/provider"
)

// norm decodes any %2F left in the project-id path segment so suffix matching
// works whether or not the server decoded it.
func norm(r *http.Request) string { return strings.ReplaceAll(r.URL.Path, "%2F", "/") }

func remediator(t *testing.T, base string) provider.Remediator {
	t.Helper()
	p, err := provider.Open("gitlab", provider.Config{BaseURL: base, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	rem, ok := p.(provider.Remediator)
	if !ok {
		t.Fatal("gitlab client does not implement Remediator")
	}
	return rem
}

func TestFixBranchProtectionUsesLiveDefault(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := norm(r)
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(p, "/projects/acme/web"):
			_, _ = w.Write([]byte(`{"default_branch":"main"}`)) // live default differs from ref
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/protected_branches"):
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &got)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+p, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	ref := core.RepoRef{Owner: "acme", Name: "web", DefaultBranch: "master"} // stale
	if err := remediator(t, srv.URL).Fix(context.Background(), ref, "branch-protection", core.Policy{}); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "main" {
		t.Fatalf("protected %q, want live default main", got["name"])
	}
}

func TestFixDefaultBranch(t *testing.T) {
	var branchQuery string
	var putBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := norm(r)
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/repository/branches"):
			branchQuery = r.URL.RawQuery
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPut && strings.HasSuffix(p, "/projects/acme/web"):
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &putBody)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+p, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	ref := core.RepoRef{Owner: "acme", Name: "web", DefaultBranch: "master"}
	if err := remediator(t, srv.URL).Fix(context.Background(), ref, "default-branch", core.Policy{DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(branchQuery, "branch=main") || !strings.Contains(branchQuery, "ref=master") {
		t.Fatalf("create branch query = %q", branchQuery)
	}
	if putBody["default_branch"] != "main" {
		t.Fatalf("edit-project body = %v", putBody)
	}
}

func gitlabFileServer(t *testing.T, existing bool, gotContent *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := norm(r)
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(p, "/merge_requests"):
			if existing {
				_, _ = w.Write([]byte(`[{"web_url":"https://gitlab.example/acme/web/-/merge_requests/1"}]`))
			} else {
				_, _ = w.Write([]byte(`[]`))
			}
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/repository/branches"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.Contains(p, "/repository/files/"):
			b, _ := io.ReadAll(r.Body)
			var body struct {
				Content string `json:"content"`
			}
			_ = json.Unmarshal(b, &body)
			*gotContent = body.Content // GitLab stores raw — capture verbatim
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/merge_requests"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"web_url":"https://gitlab.example/acme/web/-/merge_requests/2"}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+p, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fileRemediator(t *testing.T, base string) provider.FileRemediator {
	t.Helper()
	p, err := provider.Open("gitlab", provider.Config{BaseURL: base, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	fr, ok := p.(provider.FileRemediator)
	if !ok {
		t.Fatal("gitlab client does not implement FileRemediator")
	}
	return fr
}

func TestOpenRenovateMRCreates(t *testing.T) {
	var content string
	srv := gitlabFileServer(t, false, &content)
	ref := core.RepoRef{Owner: "acme", Name: "web", DefaultBranch: "main"}
	url, err := fileRemediator(t, srv.URL).OpenFix(context.Background(), ref, "dependency-automation")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(url, "/merge_requests/2") {
		t.Fatalf("MR url = %q, want .../merge_requests/2", url)
	}
	// GitLab takes raw content (not base64) — the committed body must equal the
	// config verbatim, else the file would contain a base64 blob.
	if content != string(core.RenovateConfig()) {
		t.Fatalf("committed content must be raw renovate.json, got:\n%s", content)
	}
}

func TestOpenRenovateMRIdempotent(t *testing.T) {
	var content string
	srv := gitlabFileServer(t, true, &content)
	ref := core.RepoRef{Owner: "acme", Name: "web", DefaultBranch: "main"}
	url, err := fileRemediator(t, srv.URL).OpenFix(context.Background(), ref, "dependency-automation")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(url, "/merge_requests/1") {
		t.Fatalf("expected existing MR .../merge_requests/1, got %q", url)
	}
	if content != "" {
		t.Fatalf("idempotent path should not have committed a file, got %q", content)
	}
}
