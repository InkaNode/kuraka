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
republishes `v<version>`. Every branch starts with that commit: the bump is
always the branch's first commit, before any change.

Add the version's entries to `CHANGELOG.md` (Keep a Changelog) as changes land.

## Commits

- One line, `type(scope): summary`, lowercase, no period. Types: `feat`, `fix`,
  `docs`, `chore`, `build`, `ci`, `style`, `refactor`, `test`. Scope is the area: `api`, `web`, `addons`,
  `deploy`, `readme`, etc.
- No body and no `Co-Authored-By` trailer.
- Short: the summary names one change in a few words, e.g.
  `feat(patches): add libredesk inbox switcher`.
- One commit per change: each feature, fix, patch and the changelog get their
  own commit; never list several changes in one summary.
- The version bump is the only commit without a scope (see above).
