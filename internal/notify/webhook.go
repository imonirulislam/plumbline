package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// Webhook posts the payload as JSON to a generic endpoint (NOTIFY_WEBHOOK_URL).
// Use it as the template for new channels: implement Notifier, gate Enabled()
// on an env var, and register in Notifiers.
type Webhook struct{}

func (*Webhook) Name() string  { return "webhook" }
func (*Webhook) Enabled() bool { return os.Getenv("NOTIFY_WEBHOOK_URL") != "" }

func (*Webhook) Notify(ctx context.Context, p Payload) error {
	summary := fmt.Sprintf("plumbline: %d/%d %s repos compliant", p.FullyCompliant, p.Total, p.Scope)
	if len(p.Offenders) > 0 {
		summary += fmt.Sprintf(", %d need attention", len(p.Offenders))
	}
	body, err := json.Marshal(map[string]any{
		"summary":         summary,
		"scope":           p.Scope,
		"total":           p.Total,
		"fully_compliant": p.FullyCompliant,
		"offenders":       p.Offenders,
		"report_url":      p.ReportURL,
	})
	if err != nil {
		return err
	}
	return postJSON(ctx, os.Getenv("NOTIFY_WEBHOOK_URL"), body)
}
