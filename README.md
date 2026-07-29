# plumbline

[![CI](https://github.com/imonirulislam/plumbline/actions/workflows/ci.yml/badge.svg)](https://github.com/imonirulislam/plumbline/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/imonirulislam/plumbline.svg)](https://pkg.go.dev/github.com/imonirulislam/plumbline)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

> A plumbline is the reference a builder trues their work against. **plumbline**
> audits your repositories against a policy — the same standard, every repo,
> across git hosts.

Point it at a GitHub user or org and it reports, per repo, whether the default
branch is what you expect, whether it's protected, whether CI exists, and
whether dependency automation is set up. The policy is **configurable**, and
the git host is a pluggable **connector** — GitHub today, Gitea and GitLab next.

```console
$ plumbline audit --owner acme
REPO             default-branch  branch-protection  ci      dependency-automation
acme/api         ✓ pass          ✓ pass             ✓ pass  ✗ FAIL
acme/website     ✓ pass          ✗ FAIL             ✓ pass  ✓ pass
acme/legacy      ✗ FAIL          ✗ FAIL             ✗ FAIL  ✗ FAIL

1/3 repos fully compliant.
  branch-protection         1/3
  ci                        2/3
  default-branch            2/3
  dependency-automation     1/3
```

## Why

Keeping a fleet of repositories consistent is tedious and easy to let slide.
plumbline makes the standard explicit and checkable, so drift is visible instead
of discovered later. It is **read-only by default**; remediation (opening PRs to
fix drift) is planned and will always be opt-in.

## Install

```bash
go install github.com/imonirulislam/plumbline/cmd/plumbline@latest
```

Or grab a binary from the [releases](https://github.com/imonirulislam/plumbline/releases).

## Usage

```bash
export GITHUB_TOKEN=<a token with repo:read>
plumbline audit --owner <user-or-org>

plumbline audit --owner acme --json          # machine-readable
plumbline audit --owner acme --config policy.json
plumbline audit --owner acme --fail-on-issues # exit 1 on any failure (CI gate)

# Gitea / Forgejo (self-hosted — base URL required):
export PLUMBLINE_TOKEN=<a gitea token>
plumbline audit --provider gitea --base-url https://gitea.example.com --owner acme

# GitLab (gitlab.com, or --base-url for self-hosted):
export PLUMBLINE_TOKEN=<a gitlab token>
plumbline audit --provider gitlab --owner mygroup   # a group or a username
```

### Flags

| Flag               | Default  | Meaning                                            |
| ------------------ | -------- | -------------------------------------------------- |
| `--owner`          | —        | User or org to audit (required)                    |
| `--provider`       | `github` | Connector to use                                   |
| `--config`         | —        | JSON policy file (defaults if omitted)             |
| `--base-url`       | —        | API base URL, for self-hosted instances            |
| `--json`           | off      | Emit JSON instead of a table                       |
| `--out-dir`        | —        | Also write `report.md`, `report.csv`, `summary.json` |
| `--fail-on-issues` | off      | Exit 1 if any check fails (for CI)                 |
| `--min-compliant`  | —        | Exit 1 if fewer than N repos are fully compliant (regression gate) |
| `--notify`         | off      | Send a summary to enabled notifiers (Slack / webhook via env) |
| `--workers`        | `8`      | Concurrent repo inspections                        |

Token is read from `GITHUB_TOKEN`, `GH_TOKEN`, or `PLUMBLINE_TOKEN`.

## Policy

Checks and their expected values are configurable. Defaults:

```json
{
  "default_branch": "main",
  "require_branch_protection": true,
  "require_ci": true,
  "require_dependency_automation": true
}
```

Pass a subset with `--config policy.json` to override. A check a connector can't
express is reported as **`n/s`** (not supported) — never a failure.

**Config discovery:** if `--config` is omitted, plumbline looks in the working
directory for `.plumbline.json` (then `plumbline.json`) and uses it
automatically; otherwise the built-in defaults apply. It prints which policy it
used. Copy [`.plumbline.example.json`](.plumbline.example.json) to
`.plumbline.json` to get started.

## Checks

| Check                   | Passes when …                                                 |
| ----------------------- | ------------------------------------------------------------- |
| `default-branch`        | the default branch equals `policy.default_branch`             |
| `branch-protection`     | the default branch is protected                               |
| `ci`                    | a CI configuration exists (e.g. `.github/workflows`)          |
| `dependency-automation` | Dependabot or Renovate is configured                          |

## Notifications

`--notify` sends a summary to every enabled channel (each self-gates on its env
var, so unconfigured channels are skipped):

```bash
SLACK_WEBHOOK_URL=https://hooks.slack.com/... plumbline audit --owner acme --notify
NOTIFY_WEBHOOK_URL=https://example.com/hook   plumbline audit --owner acme --notify
```

- **Slack** — a Block Kit message (header, compliant/attention counts, an
  offender list capped with "…and N more", and a "View report" button if
  `REPORT_URL` is set).
- **Webhook** — the raw payload as JSON, for any endpoint (Teams/Discord relay,
  custom service). Add a channel by implementing `notify.Notifier`.

## Remediation (`fix`)

`fix` closes drift. It re-runs the checks and, for each failing check a connector
can remediate, either plans (dry-run) or applies the change. **Dry-run by
default**; only checks that currently fail are touched (idempotent).

```bash
plumbline fix --only owner/repo                 # DRY-RUN: show what would change
plumbline fix --only owner/repo --apply         # apply to one repo
plumbline fix --owner acme                      # DRY-RUN across all of acme's repos
plumbline fix --owner acme --apply --all        # apply across the owner (explicit)
```

`--apply` is refused for a whole owner unless you pass `--all` — a single repo
(`--only`) is the safe default target. Today the GitHub connector can fix
`branch-protection` (enables a minimal protection rule on the default branch)
and `default-branch` (renames the default branch to the policy value, e.g.
`master` → `main`, retargeting open PRs); more fixes and connectors implement
the same `Remediator` capability over time.

### Run it on a schedule

See [`examples/scheduled-audit.yml`](examples/scheduled-audit.yml) — a GitHub
Actions workflow you can drop into any repo to audit an org on a cron, publish
the report as an artifact, notify Slack, and gate on `--min-compliant`.

## Architecture

Ports & adapters. Checks run against a **normalized model**; each connector
adapts its host's API into that model.

```
cmd/plumbline        CLI
internal/core        normalized model + policy (no host specifics)
internal/provider    connector port + registry
  └─ github          GitHub adapter (stdlib net/http)
internal/check       provider-agnostic checks + registry
internal/report      table / JSON output
```

Adding a connector = implement `provider.Provider` and register it. Adding a
check = add a fact to `core.RepoState`, a function to `check.Registry`, and
populate the fact in each adapter.

## Roadmap

- [x] Read-only audit (GitHub)
- [x] Gitea connector (also serves Forgejo)
- [x] GitLab connector
- [x] Notifications (Slack Block Kit + generic webhook)
- [x] Remediation: settings fixes (branch protection, default branch) — dry-run first (GitHub)
- [ ] Remediation: file fixes via PRs (CI, renovate config)
- [x] Config discovery (`.plumbline.json`, auto-loaded from the working dir)
- [ ] YAML config support

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). This project uses
[Conventional Commits](https://www.conventionalcommits.org/) and
[Keep a Changelog](https://keepachangelog.com/).

## License

[MIT](LICENSE) © Monirul Islam
