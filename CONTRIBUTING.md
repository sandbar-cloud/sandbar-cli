# Contributing to `sandbar-cli`

Thanks for your interest in contributing. This document covers how to get a
local build going, the conventions we follow, and how changes get released.

## Prerequisites

- Go 1.26 or later
- `make` (for the convenience targets)

## Building and testing

```sh
git clone https://github.com/sandbar-cloud/sandbar-cli.git
cd sandbar-cli
make build       # produces ./sandbar
make test        # runs go test ./...
```

The `cmd/` package has the Kong-based command definitions. Shared logic lives
under `internal/` (`client/` for the HTTP wrapper, `uploader/`, `hasher/`,
`config/`, `output/`, `git/`).

## Pull requests

- Write code as if the next person reading it has never seen the codebase.
- Keep changes focused. If you spot something unrelated worth fixing, open
  a separate PR.
- Add or update tests for new behavior. Unit tests live alongside their
  source files; the higher-level CLI flow is exercised in
  `integration_test.go`.
- Use the existing patterns rather than introducing new dependencies. If
  you need a new dep, call that out explicitly in the PR description.

## Commit messages

We use [Conventional Commits](https://www.conventionalcommits.org/). The
type prefix drives the next release version:

- `feat:` → minor bump
- `fix:` → patch bump
- `chore:`, `docs:`, `refactor:`, `test:`, `ci:` → no release
- `feat!:` or a `BREAKING CHANGE:` footer → major bump

Examples:

```text
feat(domains): add domains delete command
fix(deploy): retry transient 502s when finalizing
chore(deps): bump kong to v1.5.0
```

The conventional-commits pre-commit hook will reject anything that doesn't
match.

## Releases

Releases are fully automated:

1. PR merges to `main` with a `feat:`/`fix:`/etc. commit
2. `.github/workflows/release.yml` runs semantic-release, which stamps a
   tag and creates a GitHub Release
3. `.github/workflows/deploy.yml` fires on the published release, runs
   GoReleaser, uploads binaries to the release, and pushes an updated
   `Formula/sandbar.rb` to [`sandbar-cloud/homebrew-tap`](https://github.com/sandbar-cloud/homebrew-tap)

You do not (and should not) cut tags by hand.

## Reporting bugs

Open an issue with steps to reproduce, what you expected, and what
happened. CLI version (`sandbar version`) and OS/arch help a lot.

## Security

Do not file security issues in the public tracker. See
[SECURITY.md](./SECURITY.md).

## Code of Conduct

By participating, you agree to abide by the
[Code of Conduct](./CODE_OF_CONDUCT.md).
