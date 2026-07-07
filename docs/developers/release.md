# Release workflow

Releases are cut manually from `main` using [git-cliff](https://git-cliff.org/) to generate the changelog.

## Steps

Before creating a tag, run the non-publishing pre-tag gate from the repository root:

```bash
bash scripts/pre-tag-gate.sh
```

The gate runs the Go test suite, a targeted benchmark smoke check, and the server build. It does not create tags, GitHub releases, container images, or deployment side effects.

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
