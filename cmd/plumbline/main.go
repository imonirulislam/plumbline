// Command plumbline audits (and, in later versions, remediates) repositories
// across git hosts against a configurable policy.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/imonirulislam/plumbline/internal/check"
	"github.com/imonirulislam/plumbline/internal/core"
	"github.com/imonirulislam/plumbline/internal/fix"
	"github.com/imonirulislam/plumbline/internal/notify"
	"github.com/imonirulislam/plumbline/internal/provider"
	_ "github.com/imonirulislam/plumbline/internal/provider/gitea"  // register "gitea"
	_ "github.com/imonirulislam/plumbline/internal/provider/github" // register "github"
	_ "github.com/imonirulislam/plumbline/internal/provider/gitlab" // register "gitlab"
	"github.com/imonirulislam/plumbline/internal/report"
)

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "audit":
		os.Exit(cmdAudit(os.Args[2:]))
	case "fix":
		os.Exit(cmdFix(os.Args[2:]))
	case "fix-files":
		os.Exit(cmdFixFiles(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Printf("plumbline %s\n", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `plumbline %s — audit repositories against a policy, across git hosts.

Usage:
  plumbline audit --owner <user-or-org> [flags]
  plumbline fix   --only <owner/repo> [--apply]   # remediate drift (dry-run by default)
  plumbline version

Run "plumbline audit -h" or "plumbline fix -h" for flags.
`, version)
}

func cmdAudit(args []string) int {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	owner := fs.String("owner", "", "owner (user or org) whose repos to audit (required)")
	providerName := fs.String("provider", "github", "connector ("+strings.Join(provider.Names(), ", ")+")")
	configPath := fs.String("config", "", "path to a JSON or YAML policy file (defaults if omitted)")
	baseURL := fs.String("base-url", "", "API base URL, for self-hosted instances")
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	failOnIssues := fs.Bool("fail-on-issues", false, "exit 1 if any check fails")
	outDir := fs.String("out-dir", "", "also write report.md, report.csv, summary.json to this dir")
	minCompliant := fs.Int("min-compliant", -1, "exit 1 if fewer than N repos are fully compliant (regression gate)")
	notifyFlag := fs.Bool("notify", false, "send a summary to enabled notifiers (SLACK_WEBHOOK_URL / NOTIFY_WEBHOOK_URL)")
	workers := fs.Int("workers", 8, "concurrent repo inspections")
	_ = fs.Parse(args)

	if *owner == "" {
		fmt.Fprintln(os.Stderr, "audit: --owner is required")
		return 2
	}
	token := firstNonEmpty(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN"), os.Getenv("PLUMBLINE_TOKEN"))

	policy, src, err := core.DiscoverPolicy(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit:", err)
		return 2
	}
	fmt.Fprintf(os.Stderr, "policy: %s\n", policySource(src))
	prov, err := provider.Open(*providerName, provider.Config{Token: token, BaseURL: *baseURL})
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit:", err)
		return 2
	}

	ctx := context.Background()
	repos, err := prov.ListRepos(ctx, *owner)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit:", err)
		return 1
	}
	if len(repos) == 0 {
		fmt.Fprintf(os.Stderr, "no repositories found for %q\n", *owner)
		return 0
	}

	reports := make([]core.RepoReport, len(repos))
	sem := make(chan struct{}, max(1, *workers))
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, r core.RepoRef) {
			defer wg.Done()
			defer func() { <-sem }()
			var results []core.CheckResult
			if st, err := prov.Inspect(ctx, r); err != nil {
				results = check.ErrorResults(err)
			} else {
				results = check.RunAll(st, policy)
			}
			reports[i] = core.RepoReport{Repo: r.Slug(), Archived: r.Archived, Results: results}
		}(i, r)
	}
	wg.Wait()
	sort.Slice(reports, func(i, j int) bool { return reports[i].Repo < reports[j].Repo })

	if *asJSON {
		if err := report.JSON(os.Stdout, reports); err != nil {
			fmt.Fprintln(os.Stderr, "audit:", err)
			return 1
		}
	} else {
		report.Table(os.Stdout, reports, check.Names())
		report.Summary(os.Stdout, reports, check.Names())
	}

	if *outDir != "" {
		ts := time.Now().UTC().Format("2006-01-02 15:04 UTC")
		if err := report.WriteFiles(*outDir, reports, check.Names(), ts); err != nil {
			fmt.Fprintln(os.Stderr, "audit:", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "reports written to %s (report.md, report.csv, summary.json)\n", *outDir)
	}

	if *notifyFlag {
		scope := *providerName + ":" + *owner
		notify.NotifyAll(ctx, notify.FromReports(reports, scope, os.Getenv("REPORT_URL")))
	}

	if *minCompliant >= 0 {
		if fc := report.FullyCompliant(reports); fc < *minCompliant {
			fmt.Fprintf(os.Stderr, "regression: %d/%d fully compliant < required %d\n", fc, len(reports), *minCompliant)
			return 1
		}
	}
	if *failOnIssues && anyFailure(reports) {
		return 1
	}
	return 0
}

type fixEngine func(context.Context, provider.Provider, core.Policy, []core.RepoRef, bool) ([]fix.RepoFix, error)

func cmdFix(args []string) int      { return runFixCommand("fix", args, fix.Run) }
func cmdFixFiles(args []string) int { return runFixCommand("fix-files", args, fix.RunFiles) }

// runFixCommand backs both `fix` (settings) and `fix-files` (PRs); they share
// flags and flow and differ only in the remediation engine.
func runFixCommand(name string, args []string, engine fixEngine) int {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	owner := fs.String("owner", "", "owner whose repos to fix (or use --only for one repo)")
	only := fs.String("only", "", "restrict to a single owner/repo")
	providerName := fs.String("provider", "github", "connector ("+strings.Join(provider.Names(), ", ")+")")
	configPath := fs.String("config", "", "path to a JSON or YAML policy file (defaults if omitted)")
	baseURL := fs.String("base-url", "", "API base URL, for self-hosted instances")
	apply := fs.Bool("apply", false, "write changes (default: dry-run)")
	all := fs.Bool("all", false, "allow group-wide --apply (refused otherwise)")
	_ = fs.Parse(args)

	// Safety: never write to a whole owner without an explicit --all.
	if *apply && *only == "" && !*all {
		fmt.Fprintf(os.Stderr, "%s: refusing to --apply to every repo; pass --only <owner/repo> for one, or --all\n", name)
		return 2
	}
	if *owner == "" && *only == "" {
		fmt.Fprintf(os.Stderr, "%s: --owner or --only is required\n", name)
		return 2
	}

	token := firstNonEmpty(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN"), os.Getenv("PLUMBLINE_TOKEN"))
	policy, src, err := core.DiscoverPolicy(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		return 2
	}
	fmt.Fprintf(os.Stderr, "policy: %s\n", policySource(src))
	prov, err := provider.Open(*providerName, provider.Config{Token: token, BaseURL: *baseURL})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		return 2
	}

	ctx := context.Background()
	var repos []core.RepoRef
	if *only != "" {
		ref, err := resolveRepo(ctx, prov, *only)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			return 1
		}
		repos = []core.RepoRef{ref}
	} else {
		repos, err = prov.ListRepos(ctx, *owner)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			return 1
		}
	}

	fixes, err := engine(ctx, prov, policy, repos, *apply)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		return 2
	}
	fix.Print(os.Stdout, fixes, *apply)
	if fix.AnyFailed(fixes) {
		return 1
	}
	return 0
}

// resolveRepo finds a single "owner/repo" among its owner's repos (so we get
// its default branch and other attributes without a new port method).
func resolveRepo(ctx context.Context, prov provider.Provider, slug string) (core.RepoRef, error) {
	i := strings.LastIndex(slug, "/")
	if i <= 0 || i == len(slug)-1 {
		return core.RepoRef{}, fmt.Errorf("--only must be owner/repo, got %q", slug)
	}
	owner, name := slug[:i], slug[i+1:]
	repos, err := prov.ListRepos(ctx, owner)
	if err != nil {
		return core.RepoRef{}, err
	}
	for _, r := range repos {
		if r.Name == name {
			return r, nil
		}
	}
	return core.RepoRef{}, fmt.Errorf("repo %q not found under %q", name, owner)
}

func anyFailure(reports []core.RepoReport) bool {
	for _, r := range reports {
		for _, res := range r.Results {
			if res.Verdict == core.Fail || res.Verdict == core.Err {
				return true
			}
		}
	}
	return false
}

func policySource(s string) string {
	if s == "" {
		return "built-in defaults"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
