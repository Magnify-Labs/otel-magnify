# Contributing

## Developer Certificate of Origin (DCO)

Every commit must be signed off with `--signoff`:

```bash
git commit --signoff -m "feat: description"
```

By signing off, you certify that your contribution complies with the
[DCO 1.1](https://developercertificate.org) — in short, that you have the right
to submit this code under the project's license.

## Local toolchain setup

This repo uses pre-commit and pre-push hooks managed by [lefthook](https://github.com/evilmartians/lefthook). The hooks run linters and security checks before each commit/push.

### Required binaries

```bash
# macOS / Homebrew
brew install lefthook golangci-lint gitleaks hadolint

# Linux (Debian/Ubuntu) — install via each tool's release page
```

Use the Go version declared in `go.mod` when running backend checks locally. CI and Go-only pre-push tools that need a fixed toolchain (for example `govulncheck`) run inside a `golang:1.25.12` Docker container.

### Activate hooks (once per clone)

```bash
lefthook install
```

### Bypass

`git commit --no-verify` is tolerated only with a commit message explaining why. Do NOT bypass push hooks; they catch what CI would catch 2 minutes later anyway.

## Before submitting a PR

Make sure the tests pass:

```bash
# Backend
# Run from the repository root; go.mod lives at the top level.
TEST_POSTGRES_DSN='postgres://user:password@host:5432/magnify_test?sslmode=disable' go test ./...
go build ./...

# Frontend lint + build
cd frontend
npm ci
npm run lint
npm run build
```

For documentation-only changes, also run `mkdocs build --strict` after installing `docs/requirements.txt` in a virtual environment.

## Commit conventions

Format: `type: description` (in English)

| Type | Usage |
|------|-------|
| `feat:` | New feature |
| `fix:` | Bug fix |
| `docs:` | Documentation only |
| `refactor:` | Refactoring without behavior change |
| `ci:` | CI/CD |

## License

By contributing, you agree that your contribution will be licensed under
the project's [Apache License, Version 2.0](LICENSE).
