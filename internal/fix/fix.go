// Package fix remediates drift: for each repo it re-runs the checks and, for
// every failing check the connector can fix, either plans (dry-run) or applies
// the fix. It only touches checks that currently FAIL, so it's idempotent.
package fix

import (
	"context"
	"fmt"
	"io"

	"github.com/imonirulislam/plumbline/internal/check"
	"github.com/imonirulislam/plumbline/internal/core"
	"github.com/imonirulislam/plumbline/internal/provider"
)

type Status string

const (
	Would   Status = "would"
	Applied Status = "applied"
	Skipped Status = "skipped"
	Failed  Status = "failed"
)

// Item is one remediation outcome for one check.
type Item struct {
	Check  string
	Status Status
	Detail string
}

// RepoFix is all remediation outcomes for one repo.
type RepoFix struct {
	Repo  string
	Items []Item
}

// Run remediates fixable failing checks across repos. Dry-run unless apply.
// Requires the connector to implement provider.Remediator.
func Run(
	ctx context.Context,
	prov provider.Provider,
	pol core.Policy,
	repos []core.RepoRef,
	apply bool,
) ([]RepoFix, error) {
	rem, ok := prov.(provider.Remediator)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support fix", prov.Name())
	}
	fixable := make(map[string]bool)
	for _, name := range rem.FixableChecks() {
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
				continue // only fix genuine failures this connector can fix
			}
			if !apply {
				rf.Items = append(rf.Items, Item{res.Check, Would, describe(res.Check, r)})
				continue
			}
			if err := rem.Fix(ctx, r, res.Check); err != nil {
				rf.Items = append(rf.Items, Item{res.Check, Failed, err.Error()})
			} else {
				rf.Items = append(rf.Items, Item{res.Check, Applied, describe(res.Check, r)})
			}
		}
		out = append(out, rf)
	}
	return out, nil
}

func describe(checkName string, r core.RepoRef) string {
	switch checkName {
	case "branch-protection":
		return fmt.Sprintf("enable protection on %q", r.DefaultBranch)
	default:
		return ""
	}
}

var symbols = map[Status]string{
	Would:   "○ would  ",
	Applied: "● applied",
	Skipped: "· skip   ",
	Failed:  "✗ FAIL   ",
}

// Print renders the remediation plan/outcome and a summary.
func Print(w io.Writer, fixes []RepoFix, apply bool) {
	tally := map[Status]int{}
	for _, rf := range fixes {
		prefix := ""
		if !apply {
			prefix = "[dry-run] "
		}
		if len(rf.Items) == 0 {
			fmt.Fprintf(w, "%s%s\n   · nothing to fix\n", prefix, rf.Repo)
			continue
		}
		fmt.Fprintf(w, "%s%s\n", prefix, rf.Repo)
		for _, it := range rf.Items {
			tally[it.Status]++
			detail := ""
			if it.Detail != "" {
				detail = " — " + it.Detail
			}
			fmt.Fprintf(w, "   %s  %s%s\n", symbols[it.Status], it.Check, detail)
		}
	}
	fmt.Fprintf(w, "\nSummary: %d applied, %d would, %d skipped, %d failed.\n",
		tally[Applied], tally[Would], tally[Skipped], tally[Failed])
	if !apply && tally[Would] > 0 {
		fmt.Fprintln(w, "Re-run with --apply (and --only <owner/repo> or --all) to make these changes.")
	}
}

// AnyFailed reports whether any remediation failed.
func AnyFailed(fixes []RepoFix) bool {
	for _, rf := range fixes {
		for _, it := range rf.Items {
			if it.Status == Failed {
				return true
			}
		}
	}
	return false
}
