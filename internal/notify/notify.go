// Package notify delivers audit summaries to pluggable channels (Slack, generic
// webhook, …). Each Notifier self-gates on its config (an env var), so only
// configured channels fire. Add a channel by implementing Notifier and
// appending it to Notifiers.
package notify

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/imonirulislam/plumbline/internal/core"
)

// Offender is a repo that failed at least one check, with the failing checks.
type Offender struct {
	Repo    string
	Missing []string
}

// Payload is the channel-agnostic summary handed to every notifier.
type Payload struct {
	Scope          string // e.g. "github:acme"
	Total          int
	FullyCompliant int
	Offenders      []Offender
	ReportURL      string // optional link (e.g. a CI artifact)
}

// Notifier is a delivery channel.
type Notifier interface {
	Name() string
	Enabled() bool
	Notify(ctx context.Context, p Payload) error
}

// Notifiers is the registry of channels. Each fires only if Enabled().
var Notifiers = []Notifier{&Slack{}, &Webhook{}}

// FromReports builds a Payload from audit results. A repo is an offender if any
// check failed or errored.
func FromReports(reports []core.RepoReport, scope, reportURL string) Payload {
	p := Payload{Scope: scope, Total: len(reports), ReportURL: reportURL}
	for _, r := range reports {
		var missing []string
		for _, res := range r.Results {
			if res.Verdict == core.Fail || res.Verdict == core.Err {
				missing = append(missing, res.Check)
			}
		}
		if len(missing) == 0 {
			p.FullyCompliant++
		} else {
			p.Offenders = append(p.Offenders, Offender{Repo: r.Repo, Missing: missing})
		}
	}
	return p
}

// NotifyAll fires every enabled notifier; failures are logged, not fatal.
func NotifyAll(ctx context.Context, p Payload) {
	var active []Notifier
	for _, n := range Notifiers {
		if n.Enabled() {
			active = append(active, n)
		}
	}
	if len(active) == 0 {
		fmt.Fprintln(os.Stderr, "notify: no channels enabled (set SLACK_WEBHOOK_URL / NOTIFY_WEBHOOK_URL)")
		return
	}
	for _, n := range active {
		if err := n.Notify(ctx, p); err != nil {
			fmt.Fprintf(os.Stderr, "notify: [%s] %v\n", n.Name(), err)
		}
	}
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// postJSON POSTs body to url and errors on a non-2xx response.
func postJSON(ctx context.Context, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST returned HTTP %d", resp.StatusCode)
	}
	return nil
}
