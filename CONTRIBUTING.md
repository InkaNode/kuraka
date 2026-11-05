# Contributing

## Versions

Kuraka uses `MAJOR.MINOR.PATCH` (`a.b.c`), set in `internal/version/version.go`.

- **MAJOR**: breaks compatibility or changes almost everything (config, API, data, deploy).
- **MINOR**: large changes: several features, one sizeable feature, or a large fix that adds behavior.
- **PATCH**: small additions and fixes.

Bumping MINOR resets PATCH to 0; bumping MAJOR resets both.

To start a version, change `Version` and commit it alone with exactly
`chore: bump version to <version>`. The release workflow looks for that message
to know which commits belong to the release; every later CI-green push to `main`
republishes `v<version>`.

Add the version's entries to `CHANGELOG.md` (Keep a Changelog) as changes land.

## Commits

- One line, `type(scope): summary`, lowercase, no period. Types: `feat`, `fix`,
  `docs`, `chore`, `build`, `ci`, `style`, `refactor`, `test`. Scope is the area: `api`, `web`, `addons`,
  `deploy`, `readme`, etc.
- No body and no `Co-Authored-By` trailer.
- The version bump is the only commit without a scope (see above).
