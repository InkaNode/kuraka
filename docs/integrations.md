# Integrations, CI and migrations (Phases 2.5, 3 and 5)

Builds on [architecture.md](architecture.md). Same rules: tenancy is decided in `internal/authz`, every mutation is audited, stored secrets are encrypted with the secret box and never returned by the API; newly generated agent passwords are returned only once.

## Ownership

| Resource | Owner | Who manages it | Who uses it |
|---|---|---|---|
| DNS provider (Cloudflare token) | a tenant, or global (`tenant_id` NULL) | superadmin: all; tenant_admin: own tenant's | domains of the same tenant; global ones only when a superadmin assigns them |
| Helpdesk (Libredesk instance) | **always one tenant** | superadmin: all; tenant_admin: own tenant's | mailboxes of the same tenant only |
| Migration job | the mailbox's domain | anyone who can access that mailbox | |

A helpdesk is never global: one Libredesk shared by two tenants would let their agents read each other's tickets.

## Data model (migrations 0002 and 0004)

```
dns_providers   (id, tenant_id NULL→tenants, kind CHECK kind='cloudflare', name, token_enc, created_at)
domains         + dns_provider_id NULL→dns_providers ON DELETE SET NULL
helpdesks       (id, tenant_id NOT NULL→tenants, kind CHECK kind='libredesk', name, url, api_key, api_secret_enc,
                 status CHECK IN (pending,ready) DEFAULT ready, setup_secret_enc DEFAULT '', created_at)
helpdesk_links  (id, domain_id→domains ON DELETE CASCADE, account_id, helpdesk_id→helpdesks ON DELETE CASCADE,
                 inbox_id INTEGER, app_password_id TEXT, created_at, UNIQUE(account_id, helpdesk_id))
migrations      (id, domain_id→domains ON DELETE CASCADE, account_id, source_host, source_port, source_tls,
                 source_user, source_secret_enc NULL, status CHECK IN (queued,running,done,failed,cancelled),
                 folders_total, folders_done, messages_total, messages_copied, messages_skipped, error,
                 created_by NULL→users, created_at, started_at NULL, finished_at NULL)
```

`source_secret_enc` is wiped when a job ends, whatever the result.

## Cloudflare (Phase 2.5)

Package `internal/cloudflare`: `VerifyToken`, `FindZone(name)` (exact name, then parent labels), paginated `ListRecords`, `CreateRecord`, `UpdateRecord` (PATCH), `DeleteRecord`, `SetRedirect(zoneID, target)` (Single Redirects ruleset, path and query preserved). Records created by Kuraka carry the comment `managed by Kuraka`.

Package `internal/dnsplan`:

- **Desired** = `stalwart.ParseZone(domain.ZoneFile)` for the primary domain, plus `RewriteForAlias` for each alias domain.
- **Plan(desired, actual)** produces one change per desired record: `ok`, `create`, `update` (same type and name, managed value differs) or `conflict`.
  - A conflict is a record Kuraka would have to delete to apply the plan: another MX on the same name, another SPF TXT, a CNAME where an A is needed, and so on.
  - Conflicts are applied only with `replace_conflicts: true`, because replacing an MX moves the mail. directorioarenales.com still receives mail at Purelymail.
  - Records Kuraka does not manage are never touched.
- **Check** compares desired records with what the world sees: through Cloudflare when the domain has a provider, otherwise through public DNS (`net.Resolver` pointed at `KURAKA_DNS_RESOLVER`, default `1.1.1.1:53`). Each record becomes `ok`, `missing` or `different`.

## Libredesk (Phase 2.5)

Package `internal/libredesk`: `Ping`, `SystemAPIKey`, `ListInboxes`, `CreateEmailInbox`, `DeleteInbox`, `CreateAgent`, `SetAgentPassword`. Auth is `Authorization: token key:secret`.

**Connect a mailbox:**
1. Stalwart `x:AppPassword/set` with `accountId` = the mailbox creates a revocable password (verified live: IMAP login works with it).
2. `POST /api/v1/inboxes` in Libredesk: IMAP 993 TLS and SMTP 465 TLS against `KURAKA_MAIL_HOST`, username = mailbox email, password = the app password.
3. Store the link.

**Disconnect:** delete the Libredesk inbox (best effort), destroy the app password, delete the link.

## Migration tool (Phase 5)

Package `internal/migrate` copies one mailbox from any IMAP server into a Stalwart mailbox:

- **Access:**
  - Source: IMAP with the user's credentials. TLS by default; STARTTLS and plain only when chosen explicitly.
  - Target: IMAP to `KURAKA_MAIL_HOST:993`, logged in with an app password created for the job and destroyed afterwards.
- **Folders:**
  - Every source folder is copied.
  - Special-use folders (Sent, Drafts, Trash, Junk, Archive) map to the target's special-use folders; the rest keep their names, creating parents as needed.
- **Messages:** `APPEND` keeps the flags and the internal date.
- **Idempotent:** a message whose Message-ID already exists in the target folder is skipped, so a job can be re-run after a failure or to catch up before the MX switch.
- **Execution:**
  - Runs in a background worker pool (`KURAKA_MIGRATION_WORKERS`, default 2), reporting progress to the DB.
  - Cancellable.
  - A job left `running` at startup is marked `failed` with "interrupted".

## Libredesk provisioning (Phase 5)

A superadmin fills **Create helpdesk** with the tenant and public hostname. `POST /api/v1/helpdesks/provision` returns a ZIP containing `docker-compose.yml`, `config.toml`, `.env`, `desk.caddy`, `install.sh` and bilingual `README.txt`, and stores a `pending` helpdesk with its System password encrypted. Keep the ZIP and extracted secrets private.

Unzip on the VPS and run `sh install.sh`. The installer starts Libredesk **v2.9.2**, Postgres and Redis, waits up to 180 seconds for the app logs to contain `server starting`, removes `LIBREDESK_SYSTEM_USER_PASSWORD` from `.env` (only the first idempotent install needs it), copies the HTTPS site into `${KURAKA_DEPLOY:-/root/kuraka/deploy}/sites/` and reloads Caddy. Caddy and Kuraka reach `http://kuraka-<name>-desk:9000` over `kuraka_mail`; Libredesk has no published host port. Kuraka never gets the Docker socket.

The hostname must point to the VPS. When a matching tenant domain has a Cloudflare provider, Kuraka creates an unproxied CNAME to `KURAKA_MAIL_HOST`; any existing record at that name is preserved. `X-Kuraka-DNS` reports `created`, `exists`, `manual` (no provider) or `error`. A DNS failure never discards the ZIP.

Press **Activate** in Kuraka (opening Integrations also tries once for each pending row). Kuraka logs in as System, takes the returned user ID, manually carries the Secure session and CSRF cookies over internal HTTP, and creates an agent API key with `X-CSRFTOKEN`. Login itself requires no CSRF token or Origin/Referer header. Activation verifies the new key, encrypts its secret, clears the stored setup password and marks the helpdesk `ready`. Activations are serialized so concurrent calls never regenerate an already stored key. Unavailable instances return `409 helpdesk_not_ready`; pending instances cannot connect mailboxes.

The System password is only used by Kuraka once at activation. After **Activate**, delete the downloaded ZIP. System is not used by people; Libredesk retains it internally and Kuraka uses its Admin API key.

After activation, a superadmin presses **Create account** on a ready helpdesk in **Integrations**, with role **Admin** (default) or **Agent**. Libredesk always creates the System user and it cannot be deleted; Kuraka uses it through its API key, people never sign in with it. **My Kuraka account** asks for the current Kuraka password, verifies it (same rate limits as login; a wrong password is `422`, never `401`) and reuses it in Libredesk when it passes Libredesk v2.9.2 `IsStrongPassword` (10-72 bytes, upper, lower, digit and symbol); otherwise a random password is generated. **Another person** takes a name and email and always gets a generated password. Kuraka creates the agent, then sets the password in a second request because Libredesk ignores `new_password` on creation. The response is `{email, url}` plus `password` only when generated, shown once. An existing agent returns `409 conflict`; other upstream failures return `502 upstream_error`. If the password update fails after creation, the agent remains with an unknown password; an administrator must reset it in Libredesk.

**My Kuraka account** also offers a checked option, `sync` (default `true`), to keep the Libredesk password in step with the Kuraka one. Kuraka stores the agent id in `helpdesk_agents`; each later `POST /auth/password` reads the agent and writes it back with the new password, keeping its roles, teams and enabled state. The Kuraka change never fails because of Libredesk: the response adds `helpdesk_synced`, `helpdesk_weak` (the new password fails `IsStrongPassword` and was not sent) and `helpdesk_failed` counts, and the account page warns about the last two. Pending helpdesks are skipped.

The manual **Connect Libredesk** form still registers existing instances as `ready`. New mailbox forms offer a checked **Connect to Libredesk** option when the domain's tenant has a ready helpdesk. Connection failures show a warning while preserving the created mailbox and its generated password.

## Updates

Superadmins see an **Updates** card on the **System** page (`#/system`) for Kuraka itself, the Stalwart mail server ([stalwartlabs/stalwart](https://github.com/stalwartlabs/stalwart)), the official Stalwart web interface ([stalwartlabs/webui](https://github.com/stalwartlabs/webui)) and every helpdesk provisioned by Kuraka (internal URL host `kuraka-<name>-desk`; helpdesks connected by URL are not listed).

- **Check:** Kuraka reads `GET https://api.github.com/repos/{owner}/{repo}/releases/latest` (`tag_name`) for `stalwartlabs/stalwart`, `abhinavxd/libredesk` and `stalwartlabs/webui`, one minute after start and then every 24 hours, plus **Check now** (`POST /updates/check`, at most once per 30 seconds; otherwise the cached result is returned). Tags are kept in memory; a failed lookup keeps the previous tag and reports `error`.
- **States:** `current` (up to date), `available` (update found), `updating`, `error` (with a message), `unknown` (no release read yet).
- **Stalwart:** the image tag comes from `STALWART_VERSION` in `deploy/.env` (default `v1.0.2` in `deploy/docker-compose.yml`, which deploys reset to `origin/main`, so the version is never edited there). Kuraka gets the same value as `KURAKA_STALWART_VERSION`. **Update now** asks for confirmation (mail pauses for a few seconds), then writes `KURAKA_UPDATES_DIR/stalwart.json` = `{"tag"}` (`POST /updates/stalwart`) with the same checks and `409`s as Libredesk. The host timer sets `STALWART_VERSION` in `deploy/.env` (file mode kept), runs `docker compose pull stalwart`, starts it once with `STALWART_UPDATE_ROLE_PERMISSIONS=true` (adds new permissions to the default roles), waits up to 180 seconds until it is healthy or answers `/jmap/session`, then runs `docker compose up -d stalwart kuraka` without the variable. On failure it restores the previous `STALWART_VERSION` and restarts Stalwart. The result goes to `stalwart.result.json`. Update the web interface right after: the card reminds you.
- **Web interface:** Stalwart's `x:Application` `resourceUrl` always points to `.../releases/latest/download/webui.zip` (set by `deploy/setup.sh`). **Update now** runs the JMAP action `x:Action/set` `{"@type": "UpdateApps"}`, which makes Stalwart download its app bundles again, and stores the applied tag in the `settings` table (migration 0006). Until the first update the current tag is empty and shown as an available update.
- **Libredesk:** new bundles install `provision.LibredeskVersion` (v2.9.2). **Update now** asks for confirmation, then writes `KURAKA_UPDATES_DIR/libredesk-<container>.json` = `{"container", "tag"}` atomically. Only stable tags (`vX.Y.Z`) are accepted; a request already pending returns `409`, and so does an unset `KURAKA_UPDATES_DIR` (`updates_disabled`). The page re-reads the state every 10 seconds while an update runs.
- **Host timer:** `kuraka-updates.timer` (installed by `deploy/setup.sh`, every minute) runs `deploy/apply-updates.sh` as root under `flock /run/kuraka-updates.lock`. For each request it dumps the database to `backup-<UTC time>.sql` (mode 600) in the helpdesk's compose directory, changes the `libredesk/libredesk` image tag in its `docker-compose.yml`, runs `docker compose pull app` and `docker compose up -d app` (the app runs `--upgrade` on every start) and waits up to 180 seconds for `server starting`. On any failure it restores the previous tag and restarts the app. It then writes `libredesk-<container>.result.json` = `{"tag", "status": "ok"|"error", "message", "at", "current"}` (owned by the Kuraka uid 65532) and removes the request.
- **Version shown:** a pending request is `updating`; otherwise `current` from the result file, else `KURAKA_STALWART_VERSION` or `provision.LibredeskVersion`. An `error` result for the latest tag shows its message, and **Update now** retries it.
- **Kuraka:** releases come from `InkaNode/kuraka`. `.github/workflows/release.yml` runs after every successful CI push to `main` and republishes the release `v<version>` (version from `internal/version/version.go`) with every commit since `chore: bump version to <version>`, targeting the pushed commit, plus the images `<version>`, `<major>.<minor>`, `latest` (stable versions only) and `sha-<commit>`. Kuraka reads the release's `target_commitish` and `html_url` and compares them with its own `version.Version` and `version.Commit` (set at build time from the `KURAKA_COMMIT` build argument). `available` has `kind` `version` (new tag) or `commits` (same tag, new commits). **Update Kuraka** asks for confirmation (the panel is down for a few minutes, mail and helpdesks keep working) and writes `kuraka.json` = `{"tag", "commit"}` (`POST /updates/kuraka`). The host timer handles it last: it requires a full commit on `origin/main`, stops Kuraka, saves its data volume to `deploy/backups/kuraka-<UTC time>.tar`, runs `git reset --hard <commit>` in the checkout, sets `KURAKA_VERSION=sha-<commit>` in `deploy/.env`, pulls that image (or builds it from the checkout when the pull fails), runs `docker compose up -d`, reloads Caddy and waits up to 180 seconds for a healthy container. On failure it restores the checkout, `KURAKA_VERSION` and the data, then restarts. The script runs from a private copy because the checkout can replace it.
- **Maintenance page:** while Kuraka is down, Caddy answers the panel with `503` and `KURAKA_MAINTENANCE_DIR/index.html` (`deploy/maintenance/`, written by Kuraka at start and whenever the global Kuraka branding is saved: name, color, logo inlined, optional `maintenance_message`), or `deploy/maintenance.html` if Kuraka never wrote one. It reloads itself every 20 seconds.
- **Waiting for Kuraka:** `internal/addons.Supported` holds the highest `MAJOR.MINOR` of Stalwart (`1.0`), the web interface (`2.0`) and Libredesk (`2.9`) the addons were tested with. A newer release shows `waiting` with `supported`, and its update request returns `409` until a Kuraka release raises the table.
- **Security:** Kuraka never gets the Docker socket. It only writes a container name and a tag into a directory; the root script ignores everything else, requires `^kuraka-[a-z0-9-]+-desk$` and `^v[0-9]+\.[0-9]+\.[0-9]+$`, checks that the file name matches the container, skips symlinks, and takes the compose directory from the container's `com.docker.compose.project.working_dir` label, never from the request. For Stalwart it reads only the tag and always uses its own deploy directory (`KURAKA_DEPLOY`); for Kuraka only the tag and a 40-character commit that must be on `origin/main`.

## Branding

Superadmins set branding globally or for one tenant (migration 0007, tables `branding` and `branding_assets`). Each app has its own row and a shared `default` row; every empty field inherits, in this order: tenant app, tenant default, global app, global default.

- **Kuraka:** name, logo, icon, color, gradient (`gradient_from`, `gradient_to`) and banner. `GET /branding/kuraka` is public; a signed-in tenant user gets the tenant's values, everyone else the global ones.
- **Stalwart:** the same fields, global only (one shared server; tenant rows are rejected and ignored). The Stalwart addon applies them through `/_kuraka/addon/`.
- **Libredesk (`desk`):** only name, logo and favicon (`icon`). Saving `default` or `desk` pushes them to every ready helpdesk in scope (`app.site_name`, `app.logo_url`, `app.favicon_url` in its general settings, other settings unchanged), and so does activation. Push failures are reported per helpdesk and do not fail the save.
- **Images:** uploaded as a base64 data URL, at most 512 KiB, PNG, JPEG, WebP, GIF or SVG (bytes must match the declared type). Served at `/branding/assets/{id}` (absolute URLs from `KURAKA_PUBLIC_URL`) with a one-year immutable cache; SVG gets a sandbox CSP. Unreferenced uploads older than an hour are deleted on the next save.
- **Validation:** names are trimmed, at most 64 characters, no control characters; colors are `#rrggbb`, stored lowercase.

## Customization addons

Kuraka no longer ships a fork of the Stalwart web interface. The custom Caddy image (`deploy/caddy`, with `replace-response`) adds `<link rel=stylesheet href=/_kuraka/addon/<app>.css>` and `<script defer src=/_kuraka/addon/<app>.js>` before `</head>` of every HTML page of the mail host (`stalwart`) and of each provisioned helpdesk (`desk`), and proxies `/_kuraka/addon/*` to Kuraka. Public: `GET /_kuraka/addon/<app>.css|js|json` (branding as CSS variables and JSON, empty when disabled) and `POST /_kuraka/addon/<app>/report` (`{version, missing}`, rate-limited). The Stalwart addon applies name, icon, logo (header and login) and color; the desk addon applies icon, color (from Default) and the login logo. In `/admin` the Stalwart addon hides the views Kuraka manages (`x:Account/User`, `x:Tenant`, `x:Domain`, `x:DkimSignature`) and Enterprise views behind a toggle. If an expected element is missing after 5 seconds it leaves the original interface alone and reports the probe ids; the **System** page then asks to wait for a Kuraka update. Superadmins can disable each addon there (`GET /api/v1/addons`, `PUT /api/v1/addons/{app}` `{disabled}`).

## Security

Stalwart blocks addresses that look like attacks, such as many failed logins or port scans. The **Security** page (superadmins) lists those blocks with the reason, since when and until when, and lets you remove one.

- **External** addresses: **Unblock** removes the block. If the address attacks again, Stalwart blocks it again.
- **Internal** addresses (private or loopback): usually the Caddy proxy, and while it is blocked webmail can show error 502. **Fix** removes the block and adds its network (/24 for IPv4, /64 for IPv6) to Stalwart's allowed list so it is not blocked again. Kuraka works out that network from Stalwart's record, never from the browser.
- Every change reloads Stalwart's blocked list, so it takes effect at once. The audit log records the address and the trusted network.

## New configuration

| Variable | Default | Purpose |
|---|---|---|
| `KURAKA_MAIL_HOST` | `mail.example.com` style, required for helpdesk and migrations | IMAP/SMTP host given to Libredesk and used as migration target |
| `KURAKA_MAIL_TLS_SKIP_VERIFY` | `false` | lab only: self-signed certificates |
| `KURAKA_DNS_RESOLVER` | `1.1.1.1:53` | resolver for public DNS checks |
| `KURAKA_MIGRATION_WORKERS` | `2` | concurrent migration jobs |
| `KURAKA_UPDATES_DIR` | empty (Stalwart and Libredesk updates disabled) | absolute directory shared with `deploy/apply-updates.sh`; `/updates` in `deploy/docker-compose.yml` |
| `KURAKA_MAINTENANCE_DIR` | empty (no branded maintenance page) | absolute directory where Kuraka writes `index.html` for Caddy; `/maintenance` in `deploy/docker-compose.yml` |
| `KURAKA_STALWART_VERSION` | `v1.0.2` | Stalwart version shown before the first update; `STALWART_VERSION` from `deploy/.env` in `deploy/docker-compose.yml` |

## HTTP API additions (`/api/v1`)

| Method and path | Who | Purpose |
|---|---|---|
| `GET/POST /dns-providers`, `DELETE /dns-providers/{id}` | superadmin; tenant_admin for own tenant | token verified on create, never returned |
| `PUT /domains/{id}/dns-provider` | domain access + provider usable | `{provider_id|null}` |
| `GET /domains/{id}/dns/check` | domain access | per-record status and summary |
| `POST /domains/{id}/dns/plan` | domain access, provider set | dry run: changes and conflicts |
| `POST /domains/{id}/dns/apply` | domain access, provider set | `{replace_conflicts}`; returns applied changes |
| `PUT /domains/{id}/alias-redirect` | domain access, provider set | `{target}`: alias domains' web redirects to the target URL |
| `GET/POST /helpdesks`, `DELETE /helpdesks/{id}` | superadmin; tenant_admin for own tenant | credentials verified on create |
| `POST /helpdesks/provision` | superadmin | `{tenant_id, host}`; deployment ZIP and `X-Kuraka-DNS`; stores pending helpdesk |
| `POST /helpdesks/{id}/agents` | superadmin only | `{mode:"self", password, role, sync}` (`sync` defaults to `true`) or `{mode:"other", name, email, role}` (Admin default or Agent); returns 201 `{email, url}` plus a generated `password` once; ready helpdesk required |
| `GET /updates`, `POST /updates/check` | superadmin only | `{checked_at, error?, updates_dir, kuraka: {current, commit, latest, latest_commit, url, state, kind?, message?}, webui: {latest, current, state}, libredesk: {latest, helpdesks: [{id, name, current, state, message?}]}}` |
| `POST /updates/kuraka` | superadmin only | Kuraka update available (or a failed one) and a release commit; writes the request file; same body |
| `POST /updates/webui` | superadmin only | Stalwart `UpdateApps`, stores the applied tag; same body |
| `POST /helpdesks/{id}/update` | superadmin only | provisioned helpdesk with an available update (or a failed one); writes the request file; same body |
| `GET /branding/{app}` | public | `kuraka` or `stalwart`; resolved fields, images as absolute URLs, empty fields omitted |
| `GET /branding`, `PUT /branding/{app}` | superadmin only | optional `?tenant=`; GET returns `{scope, global?}`, PUT takes the fields and returns `{saved, desks: [{id, url, error?}]}` |
| `POST /branding/assets` | superadmin only | `{data: "data:<type>;base64,..."}`; returns 201 `{id}` |
| `GET /blocked-ips` | superadmin only | `[{id, address, reason, createdAt, expiresAt, internal, trust}]` |
| `DELETE /blocked-ips/{id}` | superadmin only | removes the block, trusts `trust` for internal addresses, reloads Stalwart's bans; 204 |
| `POST /helpdesks/{id}/activate` | superadmin; tenant_admin for own tenant | creates and verifies API credentials; ready helpdesk or `409 helpdesk_not_ready` |
| `GET/POST /domains/{id}/mailboxes/{mid}/helpdesk`, `DELETE .../helpdesk/{link}` | mailbox access + same-tenant helpdesk | connect and disconnect |
| `GET/POST /domains/{id}/migrations`, `POST /domains/{id}/migrations/{mig}/cancel` | domain access | `{account_id, source_host, source_port, source_tls, source_user, source_password}` |

## CI (Phase 3)

GitHub Actions, `.github/workflows/ci.yml`:

1. **go:** `gofmt -l`, `go vet`, `go test ./...`, build.
2. **web:** `node --check` on every module, plus a script that fails on `innerHTML`, inline styles or external URLs.
3. **integration** (matrix of Stalwart image tags, starting with `v1`):
   - Start Stalwart, finish bootstrap with `x:Bootstrap/set`, restart, create an API key with the returned admin.
   - Run `go test -tags integration ./internal/integration/...`: the Stalwart client contract plus tenant isolation through the real API (two tenants, cross-tenant reads, writes and aliases denied, SMTP RCPT acceptance for mailboxes, aliases and alias domains).
