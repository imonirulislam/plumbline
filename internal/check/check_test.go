package check

import (
	"testing"

	"github.com/imonirulislam/plumbline/internal/core"
)

func TestChecks(t *testing.T) {
	pol := core.DefaultPolicy() // main, protection+ci+deps required

	cases := []struct {
		name  string
		state core.RepoState
		check string
		want  core.Verdict
	}{
		{
			name:  "default branch matches",
			state: core.RepoState{Ref: core.RepoRef{DefaultBranch: "main"}},
			check: "default-branch",
			want:  core.Pass,
		},
		{
			name:  "default branch mismatch",
			state: core.RepoState{Ref: core.RepoRef{DefaultBranch: "master"}},
			check: "default-branch",
			want:  core.Fail,
		},
		{
			name:  "protection present",
			state: core.RepoState{DefaultBranchProtected: core.TriYes},
			check: "branch-protection",
			want:  core.Pass,
		},
		{
			name:  "protection absent",
			state: core.RepoState{DefaultBranchProtected: core.TriNo},
			check: "branch-protection",
			want:  core.Fail,
		},
		{
			name:  "protection unsupported never fails",
			state: core.RepoState{DefaultBranchProtected: core.TriUnsupported},
			check: "branch-protection",
			want:  core.Unsupported,
		},
		{
			name:  "ci present",
			state: core.RepoState{HasCI: core.TriYes},
			check: "ci",
			want:  core.Pass,
		},
		{
			name:  "dependency automation absent",
			state: core.RepoState{HasDependencyAutomation: core.TriNo},
			check: "dependency-automation",
			want:  core.Fail,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runByName(t, tc.check, tc.state, pol)
			if got.Verdict != tc.want {
				t.Fatalf("%s: got %q, want %q (detail: %s)", tc.check, got.Verdict, tc.want, got.Detail)
			}
		})
	}
}

func TestPolicyDisablesCheck(t *testing.T) {
	pol := core.DefaultPolicy()
	pol.RequireCI = false
	got := runByName(t, "ci", core.RepoState{HasCI: core.TriNo}, pol)
	if got.Verdict != core.Skip {
		t.Fatalf("ci disabled: got %q, want skip", got.Verdict)
	}
}

func runByName(t *testing.T, name string, st core.RepoState, p core.Policy) core.CheckResult {
	t.Helper()
	for _, c := range Registry {
		if c.Name == name {
			return c.Run(st, p)
		}
	}
	t.Fatalf("no check named %q", name)
	return core.CheckResult{}
}
