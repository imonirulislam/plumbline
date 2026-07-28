// Package check holds the provider-agnostic policy checks. Each check is a pure
// function of (normalized state, policy) — it never calls a provider. A fact a
// provider can't express yields an "unsupported" verdict, never a failure.
package check

import (
	"fmt"

	"github.com/imonirulislam/plumbline/internal/core"
)

// Check is one named policy rule.
type Check struct {
	Name string
	Run  func(core.RepoState, core.Policy) core.CheckResult
}

// Registry is the ordered set of checks run against every repo. To add a check,
// append it here (and add any facts it needs to core.RepoState + the adapters).
var Registry = []Check{
	{Name: "default-branch", Run: checkDefaultBranch},
	{Name: "branch-protection", Run: checkBranchProtection},
	{Name: "ci", Run: checkCI},
	{Name: "dependency-automation", Run: checkDependencyAutomation},
}

// Names returns the check names in registry order.
func Names() []string {
	out := make([]string, len(Registry))
	for i, c := range Registry {
		out[i] = c.Name
	}
	return out
}

// RunAll evaluates every check for one repo.
func RunAll(st core.RepoState, p core.Policy) []core.CheckResult {
	out := make([]core.CheckResult, 0, len(Registry))
	for _, c := range Registry {
		out = append(out, c.Run(st, p))
	}
	return out
}

// ErrorResults marks every check as errored (used when Inspect fails).
func ErrorResults(err error) []core.CheckResult {
	out := make([]core.CheckResult, 0, len(Registry))
	for _, c := range Registry {
		out = append(out, core.CheckResult{Check: c.Name, Verdict: core.Err, Detail: err.Error()})
	}
	return out
}

func res(name string, v core.Verdict, detail string) core.CheckResult {
	return core.CheckResult{Check: name, Verdict: v, Detail: detail}
}

// triVerdict maps a three-valued fact to a verdict, preserving "unsupported".
func triVerdict(t core.Tri) (core.Verdict, string) {
	switch t {
	case core.TriYes:
		return core.Pass, ""
	case core.TriNo:
		return core.Fail, ""
	case core.TriUnsupported:
		return core.Unsupported, "provider cannot express this fact"
	default:
		return core.Err, "fact could not be determined"
	}
}

func checkDefaultBranch(st core.RepoState, p core.Policy) core.CheckResult {
	const name = "default-branch"
	if p.DefaultBranch == "" {
		return res(name, core.Skip, "not required by policy")
	}
	if st.Ref.DefaultBranch == p.DefaultBranch {
		return res(name, core.Pass, "")
	}
	return res(name, core.Fail, fmt.Sprintf("default is %q, want %q", st.Ref.DefaultBranch, p.DefaultBranch))
}

func checkBranchProtection(st core.RepoState, p core.Policy) core.CheckResult {
	const name = "branch-protection"
	if !p.RequireBranchProtection {
		return res(name, core.Skip, "not required by policy")
	}
	v, d := triVerdict(st.DefaultBranchProtected)
	if v == core.Fail {
		d = fmt.Sprintf("default branch %q is not protected", st.Ref.DefaultBranch)
	}
	return res(name, v, d)
}

func checkCI(st core.RepoState, p core.Policy) core.CheckResult {
	const name = "ci"
	if !p.RequireCI {
		return res(name, core.Skip, "not required by policy")
	}
	v, d := triVerdict(st.HasCI)
	if v == core.Fail {
		d = "no CI configuration found"
	}
	return res(name, v, d)
}

func checkDependencyAutomation(st core.RepoState, p core.Policy) core.CheckResult {
	const name = "dependency-automation"
	if !p.RequireDependencyAutomation {
		return res(name, core.Skip, "not required by policy")
	}
	v, d := triVerdict(st.HasDependencyAutomation)
	if v == core.Fail {
		d = "no Dependabot/Renovate config found"
	}
	return res(name, v, d)
}
