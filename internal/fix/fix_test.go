package fix

import (
	"context"
	"testing"

	"github.com/imonirulislam/plumbline/internal/core"
)

// fakeProvider implements provider.Provider + provider.Remediator in memory.
type fakeProvider struct {
	state    core.RepoState
	fixCalls int
	fixErr   error
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) ListRepos(context.Context, string) ([]core.RepoRef, error) {
	return []core.RepoRef{f.state.Ref}, nil
}
func (f *fakeProvider) Inspect(context.Context, core.RepoRef) (core.RepoState, error) {
	return f.state, nil
}
func (f *fakeProvider) FixableChecks() []string { return []string{"branch-protection"} }
func (f *fakeProvider) Fix(context.Context, core.RepoRef, string, core.Policy) error {
	f.fixCalls++
	return f.fixErr
}

func failingState() core.RepoState {
	return core.RepoState{
		Ref:                     core.RepoRef{Owner: "acme", Name: "web", DefaultBranch: "main"},
		DefaultBranchProtected:  core.TriNo, // the only failing, fixable check
		HasCI:                   core.TriYes,
		HasDependencyAutomation: core.TriYes,
	}
}

func TestDryRunPlansButDoesNotApply(t *testing.T) {
	p := &fakeProvider{state: failingState()}
	fixes, err := Run(context.Background(), p, core.DefaultPolicy(), []core.RepoRef{p.state.Ref}, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.fixCalls != 0 {
		t.Fatalf("dry-run called Fix %d times, want 0", p.fixCalls)
	}
	if len(fixes) != 1 || len(fixes[0].Items) != 1 || fixes[0].Items[0].Status != Would {
		t.Fatalf("expected one 'would' item, got %+v", fixes)
	}
}

func TestApplyCallsFix(t *testing.T) {
	p := &fakeProvider{state: failingState()}
	fixes, err := Run(context.Background(), p, core.DefaultPolicy(), []core.RepoRef{p.state.Ref}, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.fixCalls != 1 {
		t.Fatalf("apply called Fix %d times, want 1", p.fixCalls)
	}
	if fixes[0].Items[0].Status != Applied {
		t.Fatalf("expected 'applied', got %q", fixes[0].Items[0].Status)
	}
}

func TestIdempotentWhenAlreadyCompliant(t *testing.T) {
	st := failingState()
	st.DefaultBranchProtected = core.TriYes // now passes → nothing to fix
	p := &fakeProvider{state: st}
	fixes, err := Run(context.Background(), p, core.DefaultPolicy(), []core.RepoRef{st.Ref}, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.fixCalls != 0 {
		t.Fatalf("compliant repo triggered %d fixes, want 0", p.fixCalls)
	}
	if len(fixes[0].Items) != 0 {
		t.Fatalf("expected no items for compliant repo, got %+v", fixes[0].Items)
	}
}
