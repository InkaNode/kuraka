# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

First release, planned as 0.1.0.

### Added

- **Updates** in Integrations (superadmins): daily GitHub release check plus **Check now** for the Stalwart web interface and each provisioned Libredesk. The web interface updates through Stalwart's `UpdateApps` action; Libredesk updates are applied by the new root `kuraka-updates.timer` (`deploy/apply-updates.sh`, database backup first, rollback on failure), so Kuraka never needs the Docker socket. New `KURAKA_UPDATES_DIR`; re-run `deploy/setup.sh` to install the timer.

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
