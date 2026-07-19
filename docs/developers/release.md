# Release workflow

Releases are cut manually from `main` using [git-cliff](https://git-cliff.org/) to generate the changelog.

## Steps

Before creating a tag, run the non-publishing pre-tag gate from the repository root:

```bash
bash scripts/pre-tag-gate.sh
```

The gate runs the Go test suite, a targeted benchmark smoke check, and the server build. It does not create tags, GitHub releases, container images, or deployment side effects.

CI also runs `scripts/postgres-lifecycle-test.sh` against real, isolated
PostgreSQL 16 and 18 containers. It verifies the signed v0.7.1 baseline,
dump/restore into a distinct PostgreSQL 18 instance, current migrations, a
second application start, and the pre-upgrade restore path. Do not replace
this gate with a mocked database.

Before upgrading a deployed environment, record the pinned application image
digest and follow the [PostgreSQL lifecycle
runbook](../operations/postgresql-lifecycle.md). A release, `helm --atomic`, or
a Goose down migration does not create or restore a database backup.

```bash
# 1. Tag the new version
git tag v0.x.y -m "release: v0.x.y"

# 2. Generate the changelog
git-cliff --output CHANGELOG.md
git add CHANGELOG.md
git commit -m "docs: update changelog for v0.x.y"

# 3. Push
git push origin main
git push origin v0.x.y

# 4. Create the GitHub release manually
#    Paste the section of CHANGELOG.md for this version as the body.
```

## Conventions

- Conventional commits (`feat:`, `fix:`, `docs:`, `refactor:`, `ci:`, `chore:`).
- Semantic versioning. During pre-1.0, any `feat:` may introduce breaking changes — callers are expected to pin minor versions.
- The `CHANGELOG.md` at the repo root is the canonical one; the docs site mirrors it via `include-markdown`.
