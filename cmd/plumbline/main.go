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

	"github.com/imonirulislam/plumbline/internal/check"
	"github.com/imonirulislam/plumbline/internal/core"
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
  plumbline version

Run "plumbline audit -h" for audit flags.
`, version)
}

func cmdAudit(args []string) int {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	owner := fs.String("owner", "", "owner (user or org) whose repos to audit (required)")
	providerName := fs.String("provider", "github", "connector ("+strings.Join(provider.Names(), ", ")+")")
	configPath := fs.String("config", "", "path to a JSON policy file (defaults if omitted)")
	baseURL := fs.String("base-url", "", "API base URL, for self-hosted instances")
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	failOnIssues := fs.Bool("fail-on-issues", false, "exit 1 if any check fails")
	workers := fs.Int("workers", 8, "concurrent repo inspections")
	_ = fs.Parse(args)

	if *owner == "" {
		fmt.Fprintln(os.Stderr, "audit: --owner is required")
		return 2
	}
	token := firstNonEmpty(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN"), os.Getenv("PLUMBLINE_TOKEN"))

	policy, err := core.LoadPolicy(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit:", err)
		return 2
	}
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

	if *failOnIssues && anyFailure(reports) {
		return 1
	}
	return 0
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
