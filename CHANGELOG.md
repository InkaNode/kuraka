# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.0] - 2026-11-17

### Added

- **Live chat for websites**: on a domain's page, **Add live chat** creates a Libredesk live chat for your website, with its name, website, language, button colour and side, logo, welcome message and the websites allowed to show it. It then shows the code to paste before `</body>`, with **Copy code** and **Tell your AI**, which copies instructions for the AI assistant that edits your website. Chats are listed under the mailboxes with **Code**, **Edit** and **Delete**.
- **Libredesk automations**: rule types, events, conditions and actions show icons, and each rule can have its own icon. Rules are listed in compact rows with an on/off switch, **Edit** and **Delete**.
- **Libredesk tags**: a tag can have one of nine colours, shown as a dot on its chips. Tags without a colour look as before.
- **Libredesk Account**: the user menu shows your photo next to **Account**, with **View photo**, **Change photo** and **Remove photo**.
- **Libredesk conversation list**: **Display as** switches between **List** and **Groups**. Groups show one section per status with its count; each section opens and closes, loads more on its own, and **Closed** starts closed. Views stay a list.
- **Libredesk sort menu**: each order has an icon, also shown on the button.
- **Libredesk sidebar**: a button next to the inbox title shrinks the sidebar to icons, with each name shown on hover, and opens it again. Team inboxes and views come back when it is opened again.
- **Libredesk team inboxes**: each team has a **Weight** from 0 to 1000. Agents sort their team inboxes as **My order** (drag, or Alt + arrow keys), **Alphabetical** or **By weight**, where higher weights go first and teams with the same weight stay together, in the agent's order or by name. Teams the agent never moved go last. **Group by weight** adds a heading per weight. The choice is saved to the agent's account.
- **Libredesk automations**: update rules can assign the conversation to the **Agent who did the action**, and **Assigned user** conditions can compare with that agent. The system, the contact and AI assistants never count as that agent.
- **Libredesk automations**: **Hours since** conditions can **Count business hours only**, using the business hours of the assigned team or the default ones from General; nights, closed days and holidays do not count. Without business hours set up, those conditions never match. A new **Hours since tag added** condition counts from when the chosen tag was added.
- **Libredesk Admin → General**: **Collapse activity events after** sets how many activity events in a row are shown before they start collapsed. The default is 2.
- **Libredesk contacts**: contacts without a photo get their own solid avatar colour, one of 40, in the conversation list, messages, the contact panel and Contacts. Each contact always keeps the same colour.

### Changed

- **Libredesk conversation**: activity events are smaller, and two or more in a row on the same day are grouped. Groups larger than the General setting start collapsed behind **Show N activity events**.
- **Libredesk patches**: the tag colour, rule icon, activity setting and team inbox order patches change the database, so they are marked required. If a Libredesk update breaks one, checkout fails and nothing releases until it is fixed.
- **System → Updates**: the list refreshes on its own every 30 seconds, so versions found by the daily check show without reloading. **Check now** says whether updates were found.
- **System → Customization**: when an addon no longer recognizes the page, it shows **Original design** with a tip on how to fix it, instead of a long message and a list of page elements.
- **System**: the page has one card each for **Kuraka**, **Mail (Stalwart)** and **Helpdesk (Libredesk)**, with its versions, updates, patches and customization. **Patches** lists every patch Kuraka adds, each with what it adds under its name, folded, and opens on its own when some could not be applied. **Check now** moved to the top of the page.

### Fixed

- **Libredesk status icons**: inside buttons the icons follow the text size.
- **System → Updates**: right after Kuraka starts, the latest versions show at once. Before, they stayed empty until the first check a minute later, even after reloading.
- **Libredesk sidebar**: the sort button of **Team inboxes** no longer covers the arrow that opens and closes it. Section titles are a little smaller and stay on one line.
- **Libredesk automations**: **Hours since** conditions no longer match when that moment has not happened yet. Before, **Hours since resolved** less than 24 also matched conversations that were never resolved.

## [0.3.2] - 2026-11-16

### Fixed

- **Libredesk email thread**: incoming emails are shown without the white box, in light and dark mode, and a long email fades into the page above **Expand**. Email backgrounds and text colours are dropped so every email follows the theme; images and links stay, links underlined.

- **Libredesk updates**: the Libredesk image is tagged with the last commit that changed its patches, so **New patches** only appears when the patches change, not after every **Update Kuraka**. Helpdesks on a 0.3.1 or 0.3.2 image are offered the update once.

- **Libredesk inbox names**: changing a mailbox's display name in Kuraka also changes its Libredesk inbox name and the sender name of the emails it sends. Existing inboxes take the name the next time the display name is saved.

### Removed

- **Libredesk Account → Profile**: the page only changed the avatar, so agents expected to edit their name there. Admins edit agents in Admin → Agents.

## [0.3.1] - 2026-11-15

### Fixed

- **Libredesk email thread**: in dark mode a long incoming email now fades to white above **Expand** instead of a dark band over the email, and the white email box is only as wide as the email.

### Changed

- **System → Updates**: Libredesk and the Stalwart web interface show their own version (`v2.9.2`, `v2.0.6`) instead of the Kuraka build tag. When only Kuraka's patches changed, the status reads **New patches** and the update is still offered.
- **Libredesk image tags**: releases publish `ghcr.io/inkanode/libredesk:<upstream>` and `:sha-<commit>` instead of `<upstream>-kuraka.<version>`. New helpdesks and **Update now** use `sha-<commit>`, which never moves, so a rollback always returns to the same image. Existing helpdesks keep working and move to the new tag on their next update.
- **README**: overview and architecture images instead of the Mermaid chart, and sections on patches, updates and security.

## [0.3.0] - 2026-11-15

### Added

- **Patched upstream builds**: `patches/stalwart-webui` and `patches/libredesk` hold Kuraka's changes as patches on an upstream tag (`scripts/patches.sh checkout|export|rebase`). CI applies and builds them; the release attaches `stalwart-webui.zip` and publishes `ghcr.io/inkanode/libredesk:<upstream>-kuraka.<version>` and `:sha-<commit>`. A daily workflow opens a pull request for each new upstream release; patches that no longer apply are skipped with the conflict explained, and CI merges it when every check passes; the release still waits for a normal push to main. System → Updates lists what does not work while a patch is skipped, marked urgent or notice.

- **Libredesk patches**: built-in status icons with a gap before the name; email conversations as a flat thread instead of chat bubbles; an inbox switcher grouped by client; automation conditions on tags (contains, not contains, equals for exactly those tags, set, not set), on tags added or removed with a new "Tags changed" event, on contact name and phone, on channel and on conversation custom fields. Saving a rule with an unknown field, operator or event returns an input error.

### Changed

- **Stalwart 1.1**: Kuraka supports Stalwart `v1.1.x`, so System → Updates offers `v1.1.0` instead of waiting. The update starts it once with `STALWART_UPDATE_ROLE_PERMISSIONS=true`, which adds the new passkey permissions to the default roles.
- **System → Updates**: helpdesks are listed as Libredesk with their domain below, and the "Waiting for Kuraka" explanation is an info icon with a tooltip instead of a long line in the table.

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

[0.3.0]: https://github.com/InkaNode/kuraka/releases/tag/v0.3.0
[0.2.0]: https://github.com/InkaNode/kuraka/releases/tag/v0.2.0
[0.1.1]: https://github.com/InkaNode/kuraka/releases/tag/v0.1.1
[0.1.0]: https://github.com/InkaNode/kuraka/releases/tag/v0.1.0
