# Notes for Claude

- Design and UI: follow Kuraka's design (`docs/ui.md`: rules, tokens, screens; `web/static/assets/app.css`; existing `core/ui.js` helpers). Use the Hallmark skill (`~/.claude/skills/hallmark`, github.com/Nutlope/hallmark) for any design or UI work: `/hallmark audit <path>` to check a screen, never a theme that replaces Kuraka's tokens.
- Versions and commits: see `CONTRIBUTING.md`. Commits are one line `type(scope): summary`, no body, no Co-Authored-By.
