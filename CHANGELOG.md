# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Patched upstream builds**: `patches/stalwart-webui` and `patches/libredesk` hold Kuraka's changes as patches on an upstream tag (`scripts/patches.sh checkout|export|rebase`). CI applies and builds them; the release attaches `stalwart-webui.zip` and publishes `ghcr.io/inkanode/libredesk:<upstream>-kuraka.<version>` and `:sha-<commit>`. A daily workflow opens a pull request for each new upstream release; patches that no longer apply are skipped with the conflict explained, and CI merges it when every check passes; the release still waits for a normal push to main. System → Updates lists what does not work while a patch is skipped, marked urgent or notice.

## [0.2.0] - 2026-11-05

### Added

- **Client onboarding wizard** (`#/onboard`, superadmins): creates or picks the client, adds or imports the domain with its alias domains, sets up DNS, creates the tenant admin and the mailboxes, installs Libredesk, adds the admin as agent and connects the mailboxes, then shows every generated password once. A retry skips the steps that already worked.

- **Automatic DNS**: a new or imported domain picks the DNS provider whose account holds its zone. `POST /api/v1/domains/{id}/dns/zone` creates the Cloudflare zone and returns its name servers; the DNS tab offers it, and Integrations shows which token permissions each case needs.

- **Libredesk install without SSH**: `POST /api/v1/helpdesks/provision` with `install: true` asks the root updates timer to render the bundle with the running Kuraka image into `deploy/desks/<name>` and start it; `GET /api/v1/helpdesks/{id}/install` reports the progress, and Kuraka activates the helpdesk when it answers. No new settings; `git pull` brings the new `deploy/apply-updates.sh`.

## [0.1.1] - 2026-11-05

### Changed

- The panel uses the Stalwart web interface fonts: Inter for text, Plus Jakarta Sans for headings and JetBrains Mono for code (latin subsets, OFL 1.1, served from `web/static/assets/vendor/fonts`).

## [0.1.0] - 2026-11-04

First release.

### Added

- **Customization addons** for the official Stalwart web interface and Libredesk, injected by a custom Caddy image (`replace-response`) and served by Kuraka under `/_kuraka/addon/`; they replace the InkaNode/stalwart-webui fork. Updates of Stalwart, the web interface or Libredesk beyond the tested `MAJOR.MINOR` wait for a Kuraka release, and the System page warns when an addon no longer recognizes the page. Re-run `deploy/setup.sh` to build Caddy, switch the web interface and route existing helpdesks through the addons.

- **Kuraka updates** on the new **System** page (superadmins), which now also holds the Stalwart and Libredesk updates from Integrations: compares the running version and commit with the latest `InkaNode/kuraka` release, by version or by new commits, and after a confirmation the root timer backs up the data, checks out the commit, installs image `sha-<commit>` (or builds it), waits for health and rolls back on failure. While Kuraka is down, Caddy shows a maintenance page with the Kuraka branding and an optional maintenance message. New `KURAKA_MAINTENANCE_DIR`; re-run `deploy/setup.sh`.

- **Release workflow**: every CI-green push to `main` republishes release `v<version>` with the commits since the version bump, plus images `<version>`, `<major>.<minor>`, `latest` and `sha-<commit>`.

- **Security** page (superadmins): lists the addresses Stalwart blocked automatically and removes a block; internal addresses (usually the Caddy proxy, which causes webmail 502 errors while blocked) also get their /24 (IPv6 /64) trusted so they are not blocked again.

- **Branding** API (superadmins): name, logo, icon, colors and banner, global or per tenant, for Kuraka, Stalwart and Libredesk with a shared default; values cascade from tenant to global (migration 0007). Libredesk gets the name, logo and favicon on save and on activation; `GET /api/v1/branding/{kuraka|stalwart}` is public for the sign-in pages.

- **Updates** in Integrations (superadmins): daily GitHub release check plus **Check now** for the Stalwart mail server, the Stalwart web interface and each provisioned Libredesk. The web interface updates through Stalwart's `UpdateApps` action; Stalwart and Libredesk updates are applied by the new root `kuraka-updates.timer` (`deploy/apply-updates.sh`, rollback on failure; Libredesk database backup first), so Kuraka never needs the Docker socket. The Stalwart version now lives in `STALWART_VERSION` in `deploy/.env` (default `v1.0.2`); its first start after an update sets `STALWART_UPDATE_ROLE_PERMISSIONS=true` once. New `KURAKA_UPDATES_DIR` and `KURAKA_STALWART_VERSION`; re-run `deploy/setup.sh` to install the timer.

- **Create account** on ready helpdesks in Integrations: Libredesk Admin or Agent for the current Kuraka user (reusing the Kuraka password when Libredesk accepts it, and by default changing it in Libredesk whenever the Kuraka password changes) or for another person, with generated passwords shown once. The installer waits for Libredesk startup and removes the System password from `.env`; delete the downloaded ZIP after activation.

- Plug-and-play Libredesk v2.9.2 setup: tenant/host form, VPS installer with Caddy HTTPS, Cloudflare CNAME provisioning, automatic API-key activation and optional connection when creating mailboxes.

- Mail queue with tenant-scoped visibility, immediate retries and cancellation.
- Metrics dashboard with Prometheus sampling, seven-day retention and 24-hour sparklines.
- Threshold alerts with firing and resolved webhooks, including Stalwart reachability, retried up to 3 times on network errors, 429 and 5xx. The payload carries `text` and `content`, so Slack and Discord webhooks work directly.
- Alert emails to enabled superadmins from the mailbox in `KURAKA_ALERT_EMAIL_FROM`.
- Multi-tenant JSON API on top of Stalwart's JMAP management API: tenants, users, domains, alias domains, mailboxes, aliases, quotas. Tenant isolation enforced in one place (`internal/authz`).
- Embedded web panel in Spanish and English.
- Guided domain setup: one "Add domain" form that can create the tenant inline and imports domains that already exist in Stalwart (API error code `domain_in_stalwart`), then a "Getting started" checklist on the domain page.
- In-app documentation page (`#/docs`).
- Authentication with argon2id passwords, sessions, CSRF/Origin checks, login rate limiting and TOTP two-factor authentication.
- Audit log of every mutating action, including denied attempts.
- Cloudflare DNS: plan, apply and check the records Stalwart requires, conflict detection, alias-domain records and web redirects.
- Libredesk helpdesk: connect mailboxes with revocable app passwords, and per-tenant deployment bundles.
- IMAP migration tool: idempotent, cancellable background jobs with progress.
- CI with GitHub Actions: Go and web checks plus integration tests against a real Stalwart.
- Deployment bundle (`deploy/`) with Caddy, Stalwart and Kuraka, and an install guide.

[0.1.1]: https://github.com/InkaNode/kuraka/releases/tag/v0.1.1
[0.1.0]: https://github.com/InkaNode/kuraka/releases/tag/v0.1.0
