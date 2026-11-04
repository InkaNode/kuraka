# Web UI (Phase 2)

The panel is plain HTML, CSS and JavaScript modules embedded in the Go binary (`web/static`). There is no build step and no framework, so the code a self-hoster runs is the code they can read.

## Rules

- **No external requests.** No CDNs, no web fonts, no analytics. The CSP is `default-src 'self'`, with no inline scripts or styles.
- **No `innerHTML` with data.** Every DOM node is built with the `h()` helper (`textContent`, attributes set one by one). Values from the API are never parsed as HTML.
- **Bilingual.** Every visible string goes through `t('key')`. Spanish and English are complete; the language comes from `localStorage`, then the browser, then Spanish.
- **Works without JS errors on 375px to 1440px.** At 768px and below, the sidebar collapses into a top menu button.
- **Accessible.** Visible focus ring, labels on every input, `aria-label` on icon buttons, 4.5:1 contrast, keyboard-operable dialogs (focus trap, Esc closes), `prefers-reduced-motion` respected.

## Design tokens

Style: Swiss minimal, dense dashboard (8px spacing grid), light and dark (`prefers-color-scheme`, overridable).

| Token | Light | Dark | Use |
|---|---|---|---|
| `--bg` | `#F8FAFC` | `#0F172A` | page |
| `--surface` | `#FFFFFF` | `#1B2336` | cards, tables, dialogs |
| `--surface-2` | `#F1F5F9` | `#272F42` | table header, hover, inputs |
| `--text` | `#0F172A` | `#F8FAFC` | body |
| `--muted` | `#475569` | `#94A3B8` | secondary text |
| `--border` | `#CBD5E1` | `#475569` | lines |
| `--accent` | `#A5431E` | `#F0875A` | primary buttons, links, active nav (Andean terracotta) |
| `--on-accent` | `#FFFFFF` | `#0F172A` | text on accent |
| `--ok` | `#15803D` | `#4ADE80` | DNS ok, enabled |
| `--warn` | `#B45309` | `#FBBF24` | pending |
| `--danger` | `#B91C1C` | `#F87171` | errors, destructive |
| `--focus` | `#2563EB` | `#93C5FD` | focus ring (2px + 2px offset) |

Type: system UI stack (`system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`), 15px base for dense tables, 1.5 line height. Monospace (`ui-monospace, "Cascadia Code", Consolas, monospace`) for DNS values, IDs and email addresses. Radius 6px. Transitions 150ms; none under reduced motion. Icons: inline SVG (Lucide paths, ISC license), `aria-hidden` when next to text.

## Screens

| Route | Who | Content |
|---|---|---|
| `#/setup` | first run | token, name, email, password |
| `#/login` | anyone | email + password, then 6-digit code step when required |
| `#/domains` | all | table: domain, alias domains, tenant (superadmin), mailbox count link |
| `#/domains/:id` | all | tabs **Mailboxes** (table + create/edit dialog), **Alias domains** (chips + add/remove), **DNS** (records per zone with copy buttons, DKIM selectors) |
| `#/tenants` | superadmin | table + create/rename/delete; per tenant: add domain (create or import) |
| `#/users` | superadmin | table + create/edit (role, tenant, disable, reset password) |
| `#/audit` | all | table: time, user, action, target, result badge, IP; "load more" |
| `#/account` | all | change password; enable/disable two-step verification with QR code |

Layout: left sidebar (logo "Kuraka", nav, language switch, user menu with logout), content area with page title and primary action on the right. Footer line: "Powered by InkaNode".

Generated passwords (new mailbox, new user, reset) appear once in a dialog with a copy button and a warning that they will not be shown again.
