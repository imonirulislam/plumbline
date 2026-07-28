# Security Policy

## Reporting a vulnerability

Please report security issues privately — **do not** open a public issue.

- Preferred: open a [private security advisory](https://github.com/imonirulislam/plumbline/security/advisories/new).
- Or email **imonirul017@gmail.com**.

Please include steps to reproduce and the impact. You'll get an acknowledgement
within a few days, and a fix or mitigation plan once the report is confirmed.

## Supported versions

plumbline is pre-1.0; only the latest release is supported. Once 1.0 ships, this
section will list supported version ranges.

## Scope note

plumbline talks to git-host APIs with a token you provide. It never transmits
your token anywhere except the configured host's API, and does not log token
values.
