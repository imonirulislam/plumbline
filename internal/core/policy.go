package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// configNames are the policy files auto-discovered in the working directory,
// in precedence order, when no explicit --config is given.
var configNames = []string{".plumbline.json", "plumbline.json"}

// Policy declares which checks run and their expected values. It is
// intentionally generic: no provider-specific concepts leak in here. Ships with
// sensible defaults; override any subset via a JSON config file.
type Policy struct {
	// DefaultBranch is the required default branch name (e.g. "main").
	// Empty string disables the default-branch check.
	DefaultBranch string `json:"default_branch"`
	// RequireBranchProtection requires the default branch to be protected.
	RequireBranchProtection bool `json:"require_branch_protection"`
	// RequireCI requires the repo to have a CI configuration.
	RequireCI bool `json:"require_ci"`
	// RequireDependencyAutomation requires a dependency-update tool
	// (Dependabot / Renovate) to be configured.
	RequireDependencyAutomation bool `json:"require_dependency_automation"`
}

// DefaultPolicy is a reasonable baseline for public repositories.
func DefaultPolicy() Policy {
	return Policy{
		DefaultBranch:               "main",
		RequireBranchProtection:     true,
		RequireCI:                   true,
		RequireDependencyAutomation: true,
	}
}

// LoadPolicy overlays a JSON config file onto DefaultPolicy. A missing path
// returns the defaults unchanged; an unreadable/invalid file is an error.
func LoadPolicy(path string) (Policy, error) {
	p := DefaultPolicy()
	if path == "" {
		return p, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return p, fmt.Errorf("read policy %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("parse policy %s: %w", path, err)
	}
	return p, nil
}

// DiscoverPolicy resolves the effective policy and reports its source. If
// explicitPath is set it wins; otherwise the working directory is searched for
// a config file (see configNames); otherwise the built-in defaults are used.
// The returned source is the config path/name, or "" for built-in defaults.
func DiscoverPolicy(explicitPath string) (policy Policy, source string, err error) {
	dir, e := os.Getwd()
	if e != nil {
		dir = "."
	}
	return discoverPolicyIn(dir, explicitPath)
}

func discoverPolicyIn(dir, explicitPath string) (Policy, string, error) {
	if explicitPath != "" {
		p, err := LoadPolicy(explicitPath)
		return p, explicitPath, err
	}
	for _, name := range configNames {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			p, err := LoadPolicy(path)
			return p, name, err
		}
	}
	return DefaultPolicy(), "", nil
}
