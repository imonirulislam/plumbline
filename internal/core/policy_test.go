package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverPolicyDefaultsWhenNoFile(t *testing.T) {
	p, src, err := discoverPolicyIn(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if src != "" {
		t.Fatalf("source = %q, want empty (built-in defaults)", src)
	}
	if p != DefaultPolicy() {
		t.Fatalf("expected default policy, got %+v", p)
	}
}

func TestDiscoverPolicyLoadsDotFileAndOverlaysDefaults(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`{"default_branch":"trunk","require_ci":false}`)
	if err := os.WriteFile(filepath.Join(dir, ".plumbline.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	p, src, err := discoverPolicyIn(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if src != ".plumbline.json" {
		t.Fatalf("source = %q, want .plumbline.json", src)
	}
	if p.DefaultBranch != "trunk" {
		t.Errorf("DefaultBranch = %q, want trunk", p.DefaultBranch)
	}
	if p.RequireCI {
		t.Error("RequireCI should be false (from config)")
	}
	if !p.RequireBranchProtection {
		t.Error("unspecified fields should keep defaults (RequireBranchProtection=true)")
	}
}

func TestLoadPolicyYAMLOverlaysDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "policy.yaml")
	body := []byte("default_branch: trunk\nrequire_ci: false\n")
	if err := os.WriteFile(cfg, body, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p.DefaultBranch != "trunk" {
		t.Errorf("DefaultBranch = %q, want trunk", p.DefaultBranch)
	}
	if p.RequireCI {
		t.Error("RequireCI should be false (from YAML)")
	}
	if !p.RequireBranchProtection {
		t.Error("unspecified fields should keep defaults (RequireBranchProtection=true)")
	}
}

func TestDiscoverPolicyLoadsYAMLDotFile(t *testing.T) {
	dir := t.TempDir()
	body := []byte("default_branch: release\nrequire_dependency_automation: false\n")
	if err := os.WriteFile(filepath.Join(dir, ".plumbline.yml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	p, src, err := discoverPolicyIn(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if src != ".plumbline.yml" {
		t.Fatalf("source = %q, want .plumbline.yml", src)
	}
	if p.DefaultBranch != "release" || p.RequireDependencyAutomation {
		t.Fatalf("YAML overlay not applied: %+v", p)
	}
}

func TestDiscoverPolicyPrefersYAMLOverJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".plumbline.json"), []byte(`{"default_branch":"from-json"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".plumbline.yaml"), []byte("default_branch: from-yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, src, err := discoverPolicyIn(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if src != ".plumbline.yaml" {
		t.Fatalf("source = %q, want .plumbline.yaml to win", src)
	}
	if p.DefaultBranch != "from-yaml" {
		t.Errorf("DefaultBranch = %q, want from-yaml", p.DefaultBranch)
	}
}

func TestDiscoverPolicyExplicitPathWins(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "custom.json")
	if err := os.WriteFile(cfg, []byte(`{"default_branch":"main"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A discoverable file that must be ignored when an explicit path is given.
	_ = os.WriteFile(filepath.Join(dir, ".plumbline.json"), []byte(`{"default_branch":"IGNORED"}`), 0o644)
	p, src, err := discoverPolicyIn(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if src != cfg {
		t.Fatalf("source = %q, want %q", src, cfg)
	}
	if p.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want main", p.DefaultBranch)
	}
}
