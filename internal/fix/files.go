package fix

import (
	"context"
	"fmt"

	"github.com/imonirulislam/plumbline/internal/check"
	"github.com/imonirulislam/plumbline/internal/core"
	"github.com/imonirulislam/plumbline/internal/provider"
)

// RunFiles remediates file-fixable failing checks by opening PRs. Dry-run unless
// apply. Requires the connector to implement provider.FileRemediator. Same
// shape as Run, so the CLI and printer are shared.
func RunFiles(
	ctx context.Context,
	prov provider.Provider,
	pol core.Policy,
	repos []core.RepoRef,
	apply bool,
) ([]RepoFix, error) {
	fr, ok := prov.(provider.FileRemediator)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support file remediation", prov.Name())
	}
	fixable := make(map[string]bool)
	for _, name := range fr.FileFixableChecks() {
		fixable[name] = true
	}

	out := make([]RepoFix, 0, len(repos))
	for _, r := range repos {
		rf := RepoFix{Repo: r.Slug()}
		st, err := prov.Inspect(ctx, r)
		if err != nil {
			rf.Items = append(rf.Items, Item{Check: "inspect", Status: Failed, Detail: err.Error()})
			out = append(out, rf)
			continue
		}
		for _, res := range check.RunAll(st, pol) {
			if res.Verdict != core.Fail || !fixable[res.Check] {
				continue
			}
			if !apply {
				rf.Items = append(rf.Items, Item{res.Check, Would, describeFile(res.Check)})
				continue
			}
			url, err := fr.OpenFix(ctx, r, res.Check)
			if err != nil {
				rf.Items = append(rf.Items, Item{res.Check, Failed, err.Error()})
			} else {
				rf.Items = append(rf.Items, Item{res.Check, Applied, url})
			}
		}
		out = append(out, rf)
	}
	return out, nil
}

func describeFile(checkName string) string {
	switch checkName {
	case "dependency-automation":
		return "open PR adding renovate.json"
	default:
		return "open PR"
	}
}
