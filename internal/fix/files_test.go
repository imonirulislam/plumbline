package fix

import (
	"context"
	"testing"

	"github.com/imonirulislam/plumbline/internal/core"
)

// fakeFileProvider implements provider.Provider + provider.FileRemediator.
type fakeFileProvider struct {
	state     core.RepoState
	openCalls int
	url       string
}

func (f *fakeFileProvider) Name() string { return "fakefile" }
func (f *fakeFileProvider) ListRepos(context.Context, string) ([]core.RepoRef, error) {
	return []core.RepoRef{f.state.Ref}, nil
}
func (f *fakeFileProvider) Inspect(context.Context, core.RepoRef) (core.RepoState, error) {
	return f.state, nil
}
func (f *fakeFileProvider) FileFixableChecks() []string { return []string{"dependency-automation"} }
func (f *fakeFileProvider) OpenFix(context.Context, core.RepoRef, string) (string, error) {
	f.openCalls++
	return f.url, nil
}

func depFailingState() core.RepoState {
	return core.RepoState{
		Ref:                     core.RepoRef{Owner: "acme", Name: "web", DefaultBranch: "main"},
		DefaultBranchProtected:  core.TriYes,
		HasCI:                   core.TriYes,
		HasDependencyAutomation: core.TriNo, // the only failing, file-fixable check
	}
}

func TestRunFilesDryRun(t *testing.T) {
	p := &fakeFileProvider{state: depFailingState(), url: "https://x/pull/1"}
	fixes, err := RunFiles(context.Background(), p, core.DefaultPolicy(), []core.RepoRef{p.state.Ref}, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.openCalls != 0 {
		t.Fatalf("dry-run opened %d PRs, want 0", p.openCalls)
	}
	if len(fixes[0].Items) != 1 || fixes[0].Items[0].Status != Would {
		t.Fatalf("expected one 'would' item, got %+v", fixes[0].Items)
	}
}

func TestRunFilesApply(t *testing.T) {
	p := &fakeFileProvider{state: depFailingState(), url: "https://x/pull/1"}
	fixes, err := RunFiles(context.Background(), p, core.DefaultPolicy(), []core.RepoRef{p.state.Ref}, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.openCalls != 1 {
		t.Fatalf("apply opened %d PRs, want 1", p.openCalls)
	}
	if fixes[0].Items[0].Status != Applied || fixes[0].Items[0].Detail != "https://x/pull/1" {
		t.Fatalf("expected applied with PR url, got %+v", fixes[0].Items[0])
	}
}

func TestRunFilesSkipsCompliant(t *testing.T) {
	st := depFailingState()
	st.HasDependencyAutomation = core.TriYes // now passes
	p := &fakeFileProvider{state: st}
	fixes, err := RunFiles(context.Background(), p, core.DefaultPolicy(), []core.RepoRef{st.Ref}, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.openCalls != 0 || len(fixes[0].Items) != 0 {
		t.Fatalf("compliant repo triggered work: calls=%d items=%+v", p.openCalls, fixes[0].Items)
	}
}
