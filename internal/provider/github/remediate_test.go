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

// TestProtectDefaultBranchUsesLiveDefault verifies branch-protection targets the
// repo's current default branch, not the (stale) ref. default-branch is
// remediated first, so by the time branch-protection runs the default may have
// already moved (master → main); we must protect the new one.
func TestProtectDefaultBranchUsesLiveDefault(t *testing.T) {
	var protectedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(p, "/repos/acme/web"):
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		case r.Method == http.MethodPut && strings.Contains(p, "/protection"):
			protectedPath = p
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+p, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	p, err := provider.Open("github", provider.Config{Token: "t", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	rem := p.(provider.Remediator)
	ref := core.RepoRef{Owner: "acme", Name: "web", DefaultBranch: "master"} // stale
	if err := rem.Fix(context.Background(), ref, "branch-protection", core.Policy{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(protectedPath, "/branches/main/protection") {
		t.Fatalf("protected %q, want the live default .../branches/main/protection", protectedPath)
	}
}
