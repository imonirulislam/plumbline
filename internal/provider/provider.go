// Package provider defines the connector port: the interface every git-host
// adapter (GitHub, Gitea, GitLab, …) implements, plus a registry so adapters
// self-register and the CLI can open one by name. Adapters keep any SDK types
// behind this boundary; the rest of plumbline only sees the normalized model.
package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/imonirulislam/plumbline/internal/core"
)

// Provider is a read (v1) connector to a git host.
type Provider interface {
	// Name is the connector's registered name (e.g. "github").
	Name() string
	// ListRepos returns the repos owned by owner (a user or org).
	ListRepos(ctx context.Context, owner string) ([]core.RepoRef, error)
	// Inspect gathers the normalized facts checks need for one repo.
	Inspect(ctx context.Context, ref core.RepoRef) (core.RepoState, error)
}

// Remediator is an optional capability: a connector that can fix drift. A
// connector advertises which checks it can remediate; the fix engine only ever
// calls Fix for a check the connector listed and that a repo currently fails.
type Remediator interface {
	// FixableChecks lists the check names this connector can remediate.
	FixableChecks() []string
	// Fix remediates the named check for one repo. Called only on --apply.
	Fix(ctx context.Context, ref core.RepoRef, check string) error
}

// Config configures a connector. BaseURL is optional (for self-hosted
// instances / Gitea); Token authenticates API calls.
type Config struct {
	Token   string
	BaseURL string
}

// Factory builds a Provider from Config.
type Factory func(Config) (Provider, error)

var registry = map[string]Factory{}

// Register makes a connector available by name. Adapters call this from init().
func Register(name string, f Factory) { registry[name] = f }

// Open constructs the named connector.
func Open(name string, cfg Config) (Provider, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (available: %v)", name, Names())
	}
	return f(cfg)
}

// Names lists registered connectors.
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
