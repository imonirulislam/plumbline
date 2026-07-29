package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Slack posts a Block Kit message via an incoming webhook (SLACK_WEBHOOK_URL).
type Slack struct{}

func (*Slack) Name() string  { return "slack" }
func (*Slack) Enabled() bool { return os.Getenv("SLACK_WEBHOOK_URL") != "" }

func (*Slack) Notify(ctx context.Context, p Payload) error {
	body, err := json.Marshal(buildSlackMessage(p))
	if err != nil {
		return err
	}
	return postJSON(ctx, os.Getenv("SLACK_WEBHOOK_URL"), body)
}

const maxOffenderRows = 40

// buildSlackMessage renders the payload as a Slack webhook body: a plain-text
// fallback plus Block Kit blocks (header, summary fields, offender sections
// capped with an "…and N more" note, and an optional report button).
func buildSlackMessage(p Payload) map[string]any {
	allGreen := len(p.Offenders) == 0

	var fallback string
	if allGreen {
		fallback = fmt.Sprintf("plumbline: all %d %s repos compliant", p.Total, p.Scope)
	} else {
		fallback = fmt.Sprintf("plumbline: %d/%d %s repos compliant, %d need attention",
			p.FullyCompliant, p.Total, p.Scope, len(p.Offenders))
	}

	header := "✅ plumbline: all compliant"
	if !allGreen {
		header = "🚨 plumbline: action needed"
	}
	fields := []map[string]any{
		{"type": "mrkdwn", "text": fmt.Sprintf("*Scope*\n`%s`", p.Scope)},
		{"type": "mrkdwn", "text": fmt.Sprintf("*Compliant*\n%d/%d", p.FullyCompliant, p.Total)},
	}
	if !allGreen {
		fields = append(fields, map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*Need attention*\n%d", len(p.Offenders))})
	}

	blocks := []map[string]any{
		{"type": "header", "text": map[string]any{"type": "plain_text", "text": header, "emoji": true}},
		{"type": "section", "fields": fields},
	}

	if !allGreen {
		blocks = append(blocks, map[string]any{"type": "divider"})
		shown := p.Offenders
		if len(shown) > maxOffenderRows {
			shown = shown[:maxOffenderRows]
		}
		for _, chunk := range chunkOffenders(shown) {
			blocks = append(blocks, map[string]any{
				"type": "section",
				"text": map[string]any{"type": "mrkdwn", "text": chunk},
			})
		}
		if len(p.Offenders) > len(shown) {
			blocks = append(blocks, map[string]any{
				"type":     "context",
				"elements": []map[string]any{{"type": "mrkdwn", "text": fmt.Sprintf("…and %d more — see the full report.", len(p.Offenders)-len(shown))}},
			})
		}
	}

	if p.ReportURL != "" {
		blocks = append(blocks, map[string]any{
			"type": "actions",
			"elements": []map[string]any{{
				"type":      "button",
				"text":      map[string]any{"type": "plain_text", "text": "View report", "emoji": true},
				"url":       p.ReportURL,
				"action_id": "view_report",
			}},
		})
	}

	return map[string]any{"text": fallback, "blocks": blocks}
}

// chunkOffenders groups offender lines into section-sized text blocks (Slack's
// section text limit is 3000 chars).
func chunkOffenders(offenders []Offender) []string {
	const limit = 2800
	var chunks []string
	var b []byte
	for _, o := range offenders {
		line := fmt.Sprintf("• `%s` — _%s_\n", o.Repo, strings.Join(o.Missing, ", "))
		if len(b) > 0 && len(b)+len(line) > limit {
			chunks = append(chunks, string(b))
			b = b[:0]
		}
		b = append(b, line...)
	}
	if len(b) > 0 {
		chunks = append(chunks, string(b))
	}
	return chunks
}
