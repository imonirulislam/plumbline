# Changelog

All notable changes to this project are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Initial release: read-only `audit` command for GitHub.
- Provider-agnostic core model with a pluggable connector port (GitHub first).
- Configurable policy (`default-branch`, `branch-protection`, `ci`,
  `dependency-automation`); checks a connector can't express report as
  "unsupported" rather than failing.
- Table and JSON output; `--fail-on-issues` gate for CI.
