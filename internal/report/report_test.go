package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/imonirulislam/plumbline/internal/core"
)

func TestWriteFilesAndSummary(t *testing.T) {
	dir := t.TempDir()
	checks := []string{"ci", "branch-protection"}
	reports := []core.RepoReport{
		{Repo: "acme/a", Results: []core.CheckResult{
			{Check: "ci", Verdict: core.Pass},
			{Check: "branch-protection", Verdict: core.Fail},
		}},
		{Repo: "acme/b", Results: []core.CheckResult{
			{Check: "ci", Verdict: core.Pass},
			{Check: "branch-protection", Verdict: core.Pass},
		}},
	}

	if err := WriteFiles(dir, reports, checks, "ts"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"report.md", "report.csv", "summary.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s SummaryData
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("summary.json not valid JSON: %v", err)
	}
	if s.Total != 2 {
		t.Errorf("total = %d, want 2", s.Total)
	}
	if s.FullyCompliant != 1 {
		t.Errorf("fully_compliant = %d, want 1 (only acme/b)", s.FullyCompliant)
	}
	if s.PerCheck["ci"] != 2 {
		t.Errorf("per_check[ci] = %d, want 2", s.PerCheck["ci"])
	}
	if s.PerCheck["branch-protection"] != 1 {
		t.Errorf("per_check[branch-protection] = %d, want 1", s.PerCheck["branch-protection"])
	}
	if s.VerdictCounts["fail"] != 1 {
		t.Errorf("verdict_counts[fail] = %d, want 1", s.VerdictCounts["fail"])
	}
}
