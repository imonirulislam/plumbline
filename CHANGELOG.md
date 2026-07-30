# Changelog

All notable changes to this project are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Remediation now works on **all three connectors**: `Remediator`
  (`branch-protection`, `default-branch`) and `FileRemediator`
  (`dependency-automation` → renovate.json) are implemented for Gitea and GitLab
  as well as GitHub. Gitea/GitLab lack an atomic branch rename, so
  `default-branch` creates the target from the current default and switches the
  default pointer. The `branch-protection` fix re-reads the repo's live default
  branch, so a repo failing both `default-branch` and `branch-protection`
  converges in a single `--apply`.
- `fix-files` command: file-based remediation via PRs, using a new optional
  `provider.FileRemediator` capability. v1 fixes `dependency-automation` on
  GitHub by opening a PR that adds a generic `renovate.json`. Dry-run by
  default; `--only`/`--all` scoping; idempotent (reuses an already-open PR). CI
  workflows are intentionally not generated (too stack-specific).
- `fix` can now remediate `default-branch` on GitHub: renames the current
  default branch to the policy value (e.g. `master` → `main`), which retargets
  open PRs. (Same dry-run/`--apply`/`--only`/`--all` safety as branch-protection.)
- `examples/scheduled-audit.yml`: a reusable GitHub Actions workflow to run
  plumbline on a schedule (artifact + Slack + `--min-compliant` gate).
- Notifications: `--notify` sends a summary to enabled channels — **Slack**
  (Block Kit) and a **generic JSON webhook** — via a pluggable `notify.Notifier`
  registry. Each channel self-gates on its env var (`SLACK_WEBHOOK_URL` /
  `NOTIFY_WEBHOOK_URL`).
- `--out-dir`: write `report.md`, `report.csv`, and `summary.json`
  (machine-readable counts: totals, per-check, verdict tallies) alongside the
  usual stdout output.
- `--min-compliant N`: regression gate — exit 1 if fewer than N repos are fully
  compliant (a repo is compliant when it has no failing/errored checks).
- Config discovery: when `--config` is omitted, `audit`/`fix` auto-load
  `.plumbline.json` (then `plumbline.json`) from the working directory, else use
  built-in defaults. The effective policy source is printed. Added
  `.plumbline.example.json`.
- `fix` command: remediate drift via a new optional `Remediator` connector
  capability. **Dry-run by default**; `--apply` writes; `--only <owner/repo>`
  targets one repo; group-wide `--apply` is refused without `--all`. Idempotent
  (only currently-failing checks are touched). v1 fixes `branch-protection` on
  GitHub (enables a minimal protection rule on the default branch).
- GitLab connector (`--provider gitlab`; gitlab.com or self-hosted via
  `--base-url`). Owner may be a group (recurses subgroups) or a user. CI is
  detected via `.gitlab-ci.yml`; dependency-automation via Renovate.
- Gitea connector (`--provider gitea --base-url …`; also serves Forgejo).
  Proves the connector abstraction with a second provider; CI detection covers
  Gitea Actions (`.gitea/workflows`), GitHub Actions, and Drone/Woodpecker.
- Initial release: read-only `audit` command for GitHub.
- Provider-agnostic core model with a pluggable connector port (GitHub first).
- Configurable policy (`default-branch`, `branch-protection`, `ci`,
  `dependency-automation`); checks a connector can't express report as
  "unsupported" rather than failing.
- Table and JSON output; `--fail-on-issues` gate for CI.
