// Package report renders audit results as a human table or machine JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/imonirulislam/plumbline/internal/core"
)

func symbol(v core.Verdict) string {
	switch v {
	case core.Pass:
		return "✓ pass"
	case core.Fail:
		return "✗ FAIL"
	case core.Unsupported:
		return "– n/s"
	case core.Skip:
		return "· skip"
	case core.Err:
		return "! err"
	default:
		return string(v)
	}
}

// Table writes an aligned table: one row per repo, one column per check.
func Table(w io.Writer, reports []core.RepoReport, checks []string) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprint(tw, "REPO")
	for _, c := range checks {
		fmt.Fprintf(tw, "\t%s", c)
	}
	fmt.Fprintln(tw)
	for _, r := range reports {
		fmt.Fprint(tw, r.Repo)
		byName := make(map[string]core.Verdict, len(r.Results))
		for _, res := range r.Results {
			byName[res.Check] = res.Verdict
		}
		for _, c := range checks {
			fmt.Fprintf(tw, "\t%s", symbol(byName[c]))
		}
		fmt.Fprintln(tw)
	}
	_ = tw.Flush()
}

// Summary writes a one-line-per-check compliance tally plus a headline.
func Summary(w io.Writer, reports []core.RepoReport, checks []string) {
	pass := map[string]int{}
	total := len(reports)
	fullyCompliant := 0
	for _, r := range reports {
		allPass := true
		for _, res := range r.Results {
			if res.Verdict == core.Pass {
				pass[res.Check]++
			}
			if res.Verdict == core.Fail || res.Verdict == core.Err {
				allPass = false
			}
		}
		if allPass {
			fullyCompliant++
		}
	}
	fmt.Fprintf(w, "\n%d/%d repos fully compliant.\n", fullyCompliant, total)
	names := append([]string(nil), checks...)
	sort.Strings(names)
	for _, c := range names {
		fmt.Fprintf(w, "  %-24s %d/%d\n", c, pass[c], total)
	}
}

// JSON writes the reports as indented JSON.
func JSON(w io.Writer, reports []core.RepoReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(reports)
}
