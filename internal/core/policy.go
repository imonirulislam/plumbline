package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// configNames are the policy files auto-discovered in the working directory,
// in precedence order, when no explicit --config is given. YAML is preferred
// over JSON when both are present, and dotfiles over their plain counterparts.
var configNames = []string{
	".plumbline.yaml", ".plumbline.yml", ".plumbline.json",
	"plumbline.yaml", "plumbline.yml", "plumbline.json",
}

// Policy declares which checks run and their expected values. It is
// intentionally generic: no provider-specific concepts leak in here. Ships with
// sensible defaults; override any subset via a JSON or YAML config file. The
// keys are identical in both formats (json and yaml tags match).
type Policy struct {
	// DefaultBranch is the required default branch name (e.g. "main").
	// Empty string disables the default-branch check.
	DefaultBranch string `json:"default_branch" yaml:"default_branch"`
	// RequireBranchProtection requires the default branch to be protected.
	RequireBranchProtection bool `json:"require_branch_protection" yaml:"require_branch_protection"`
	// RequireCI requires the repo to have a CI configuration.
	RequireCI bool `json:"require_ci" yaml:"require_ci"`
	// RequireDependencyAutomation requires a dependency-update tool
	// (Dependabot / Renovate) to be configured.
	RequireDependencyAutomation bool `json:"require_dependency_automation" yaml:"require_dependency_automation"`
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

// LoadPolicy overlays a config file onto DefaultPolicy. The format is chosen by
// extension: .yaml/.yml is parsed as YAML, anything else as JSON. Only keys
// present in the file are overridden. A missing path returns the defaults
// unchanged; an unreadable/invalid file is an error.
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
	if isYAML(path) {
		if err := yaml.Unmarshal(data, &p); err != nil {
			return p, fmt.Errorf("parse policy %s: %w", path, err)
		}
	} else {
		if err := json.Unmarshal(data, &p); err != nil {
			return p, fmt.Errorf("parse policy %s: %w", path, err)
		}
	}
	return p, nil
}

// isYAML reports whether path should be parsed as YAML (by extension).
func isYAML(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return true
	default:
		return false
	}
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
