package core

import (
	"encoding/json"
	"fmt"
	"os"
)

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
