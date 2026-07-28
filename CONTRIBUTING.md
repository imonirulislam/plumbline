# Contributing to plumbline

Thanks for your interest! Contributions are welcome.

## Development

Requires Go 1.23+.

```bash
git clone https://github.com/imonirulislam/plumbline
cd plumbline
make build      # or: go build ./...
make test       # go test ./...
make lint       # golangci-lint run  (see .golangci.yml)
make run ARGS="audit --owner <you>"
```

No runtime dependencies — the standard library only.

## Commit messages — Conventional Commits

This repo uses [Conventional Commits](https://www.conventionalcommits.org/).
PR titles are checked in CI. Format:

```
<type>(<optional scope>): <description>
```

Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `ci`, `perf`.
Examples:

```
feat(gitea): add Gitea connector
fix(github): follow pagination on large orgs
docs: document the --config flag
```

## Branches & PRs

- Branch from `main` using a `feat/…`, `fix/…`, `docs/…`, etc. name.
- Keep PRs focused; update `CHANGELOG.md` under `## [Unreleased]`.
- CI must be green (build, vet, `gofmt`, tests, lint).

## Adding a connector

Implement `provider.Provider` in `internal/provider/<name>/`, register it from
`init()`, and blank-import it in `cmd/plumbline/main.go`. Keep any SDK types
behind the port — the rest of the codebase only sees `internal/core`.

## Adding a check

Add the fact to `core.RepoState`, a function to `check.Registry`, and populate
the fact in every connector's `Inspect`. A connector that can't determine a fact
must return `core.TriUnsupported` (never a false failure).

## Code of Conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md).
