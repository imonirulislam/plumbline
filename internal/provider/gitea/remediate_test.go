package gitea

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imonirulislam/plumbline/internal/core"
	"github.com/imonirulislam/plumbline/internal/provider"
)

func remediator(t *testing.T, base string) provider.Remediator {
	t.Helper()
	p, err := provider.Open("gitea", provider.Config{BaseURL: base, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	rem, ok := p.(provider.Remediator)
	if !ok {
		t.Fatal("gitea client does not implement Remediator")
	}
	return rem
}

// TestFixBranchProtectionUsesLiveDefault checks that we protect whatever the
// repo's current default branch is — not the (possibly stale) ref we were
// handed, since default-branch is remediated first.
func TestFixBranchProtectionUsesLiveDefault(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(p, "/repos/acme/web"):
			_, _ = w.Write([]byte(`{"default_branch":"main"}`)) // live default differs from ref
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/branch_protections"):
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
	if got["rule_name"] != "main" || got["branch_name"] != "main" {
		t.Fatalf("protected the wrong branch: %v (want live default main)", got)
	}
}

func TestFixDefaultBranch(t *testing.T) {
	var createBody, patchBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/branches"):
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &createBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPatch && strings.HasSuffix(p, "/repos/acme/web"):
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &patchBody)
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
	if createBody["new_branch_name"] != "main" || createBody["old_ref_name"] != "master" {
		t.Fatalf("create branch body = %v", createBody)
	}
	if patchBody["default_branch"] != "main" {
		t.Fatalf("patch body = %v", patchBody)
	}
}

// giteaFileServer mocks the endpoints openRenovatePR walks. existing controls
// the idempotency path; gotContent captures the (base64) file body.
func giteaFileServer(t *testing.T, existing bool, gotContent *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(p, "/pulls"):
			if existing {
				_, _ = w.Write([]byte(`[{"html_url":"https://gitea.example/acme/web/pulls/1","head":{"ref":"plumbline/dependency-automation"}}]`))
			} else {
				_, _ = w.Write([]byte(`[]`))
			}
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/branches"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.Contains(p, "/contents/"):
			b, _ := io.ReadAll(r.Body)
			var body struct {
				Content string `json:"content"`
			}
			_ = json.Unmarshal(b, &body)
			if dec, err := base64.StdEncoding.DecodeString(body.Content); err == nil {
				*gotContent = string(dec)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/pulls"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"html_url":"https://gitea.example/acme/web/pulls/2"}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+p, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fileRemediator(t *testing.T, base string) provider.FileRemediator {
	t.Helper()
	p, err := provider.Open("gitea", provider.Config{BaseURL: base, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	fr, ok := p.(provider.FileRemediator)
	if !ok {
		t.Fatal("gitea client does not implement FileRemediator")
	}
	return fr
}

func TestOpenRenovatePRCreates(t *testing.T) {
	var content string
	srv := giteaFileServer(t, false, &content)
	ref := core.RepoRef{Owner: "acme", Name: "web", DefaultBranch: "main"}
	url, err := fileRemediator(t, srv.URL).OpenFix(context.Background(), ref, "dependency-automation")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(url, "/pulls/2") {
		t.Fatalf("PR url = %q, want .../pulls/2", url)
	}
	if content != string(core.RenovateConfig()) {
		t.Fatalf("committed content mismatch:\n%s", content)
	}
}

func TestOpenRenovatePRIdempotent(t *testing.T) {
	var content string
	srv := giteaFileServer(t, true, &content)
	ref := core.RepoRef{Owner: "acme", Name: "web", DefaultBranch: "main"}
	url, err := fileRemediator(t, srv.URL).OpenFix(context.Background(), ref, "dependency-automation")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(url, "/pulls/1") {
		t.Fatalf("expected existing PR .../pulls/1, got %q", url)
	}
	if content != "" {
		t.Fatalf("idempotent path should not have committed a file, got %q", content)
	}
}
