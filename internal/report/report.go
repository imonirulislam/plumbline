// Package report renders audit results as a human table or machine JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// Compliant reports whether a repo has no failing or errored checks.
func Compliant(r core.RepoReport) bool {
	for _, res := range r.Results {
		if res.Verdict == core.Fail || res.Verdict == core.Err {
			return false
		}
	}
	return true
}

// FullyCompliant counts repos with no failing/errored checks.
func FullyCompliant(reports []core.RepoReport) int {
	n := 0
	for _, r := range reports {
		if Compliant(r) {
			n++
		}
	}
	return n
}

// SummaryData is the machine-readable rollup written to summary.json.
type SummaryData struct {
	GeneratedAt    string         `json:"generated_at"`
	Total          int            `json:"total"`
	FullyCompliant int            `json:"fully_compliant"`
	PerCheck       map[string]int `json:"per_check"`      // pass count per check
	VerdictCounts  map[string]int `json:"verdict_counts"` // total cells per verdict
}

// BuildSummary tallies reports into a SummaryData.
func BuildSummary(reports []core.RepoReport, checks []string, generatedAt string) SummaryData {
	s := SummaryData{
		GeneratedAt:    generatedAt,
		Total:          len(reports),
		FullyCompliant: FullyCompliant(reports),
		PerCheck:       map[string]int{},
		VerdictCounts:  map[string]int{},
	}
	for _, c := range checks {
		s.PerCheck[c] = 0
	}
	for _, r := range reports {
		for _, res := range r.Results {
			s.VerdictCounts[string(res.Verdict)]++
			if res.Verdict == core.Pass {
				s.PerCheck[res.Check]++
			}
		}
	}
	return s
}

// WriteFiles writes report.md, report.csv, and summary.json into dir.
func WriteFiles(dir string, reports []core.RepoReport, checks []string, generatedAt string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(markdown(reports, checks, generatedAt)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.csv"), []byte(csv(reports, checks)), 0o644); err != nil {
		return err
	}
	data, err := json.MarshalIndent(BuildSummary(reports, checks, generatedAt), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "summary.json"), append(data, '\n'), 0o644)
}

func markdown(reports []core.RepoReport, checks []string, generatedAt string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# plumbline report\n\n_Generated: %s_\n\n", generatedAt)
	fmt.Fprintf(&b, "**%d/%d** repositories fully compliant.\n\n", FullyCompliant(reports), len(reports))
	fmt.Fprintf(&b, "| Repo | %s |\n|%s\n", strings.Join(checks, " | "), strings.Repeat("---|", len(checks)+1))
	for _, r := range reports {
		byName := verdictsByName(r)
		fmt.Fprintf(&b, "| %s", r.Repo)
		for _, c := range checks {
			fmt.Fprintf(&b, " | %s", string(byName[c]))
		}
		fmt.Fprint(&b, " |\n")
	}
	return b.String()
}

func csv(reports []core.RepoReport, checks []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "repo,%s\n", strings.Join(checks, ","))
	for _, r := range reports {
		byName := verdictsByName(r)
		fmt.Fprint(&b, r.Repo)
		for _, c := range checks {
			fmt.Fprintf(&b, ",%s", string(byName[c]))
		}
		fmt.Fprint(&b, "\n")
	}
	return b.String()
}

func verdictsByName(r core.RepoReport) map[string]core.Verdict {
	m := make(map[string]core.Verdict, len(r.Results))
	for _, res := range r.Results {
		m[res.Check] = res.Verdict
	}
	return m
}
