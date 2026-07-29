package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imonirulislam/plumbline/internal/core"
)

func sampleReports() []core.RepoReport {
	return []core.RepoReport{
		{Repo: "acme/a", Results: []core.CheckResult{
			{Check: "ci", Verdict: core.Pass},
			{Check: "branch-protection", Verdict: core.Fail},
		}},
		{Repo: "acme/b", Results: []core.CheckResult{
			{Check: "ci", Verdict: core.Pass},
			{Check: "branch-protection", Verdict: core.Pass},
		}},
	}
}

func TestFromReports(t *testing.T) {
	p := FromReports(sampleReports(), "github:acme", "")
	if p.Total != 2 {
		t.Errorf("total = %d, want 2", p.Total)
	}
	if p.FullyCompliant != 1 {
		t.Errorf("fully compliant = %d, want 1", p.FullyCompliant)
	}
	if len(p.Offenders) != 1 || p.Offenders[0].Repo != "acme/a" {
		t.Fatalf("offenders = %+v, want [acme/a]", p.Offenders)
	}
	if got := strings.Join(p.Offenders[0].Missing, ","); got != "branch-protection" {
		t.Errorf("missing = %q, want branch-protection", got)
	}
}

func captureServer(t *testing.T) (*httptest.Server, *[]byte) {
	t.Helper()
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &body
}

func TestSlackNotifyPostsBlockKit(t *testing.T) {
	srv, body := captureServer(t)
	t.Setenv("SLACK_WEBHOOK_URL", srv.URL)
	s := &Slack{}
	if !s.Enabled() {
		t.Fatal("Slack should be enabled when SLACK_WEBHOOK_URL is set")
	}
	if err := s.Notify(context.Background(), FromReports(sampleReports(), "github:acme", "")); err != nil {
		t.Fatal(err)
	}
	var msg map[string]any
	if err := json.Unmarshal(*body, &msg); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if msg["text"] == nil {
		t.Error("missing fallback text")
	}
	if _, ok := msg["blocks"]; !ok {
		t.Error("missing blocks")
	}
	if !strings.Contains(string(*body), "acme/a") {
		t.Error("offender acme/a not present in message")
	}
}

func TestWebhookNotifyPostsPayload(t *testing.T) {
	srv, body := captureServer(t)
	t.Setenv("NOTIFY_WEBHOOK_URL", srv.URL)
	if err := (&Webhook{}).Notify(context.Background(), FromReports(sampleReports(), "github:acme", "")); err != nil {
		t.Fatal(err)
	}
	s := string(*body)
	if !strings.Contains(s, "fully_compliant") || !strings.Contains(s, "acme/a") {
		t.Errorf("webhook body missing expected content: %s", s)
	}
}

func TestNotifyAllNoChannels(t *testing.T) {
	t.Setenv("SLACK_WEBHOOK_URL", "")
	t.Setenv("NOTIFY_WEBHOOK_URL", "")
	// Should simply no-op (log to stderr) without panicking.
	NotifyAll(context.Background(), FromReports(sampleReports(), "x", ""))
}
