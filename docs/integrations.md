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

The manual **Connect Libredesk** form still registers existing instances as `ready`. New mailbox forms offer a checked **Connect to Libredesk** option when the domain's tenant has a ready helpdesk. Connection failures show a warning while preserving the created mailbox and its generated password.

## New configuration

| Variable | Default | Purpose |
|---|---|---|
| `KURAKA_MAIL_HOST` | `mail.example.com` style, required for helpdesk and migrations | IMAP/SMTP host given to Libredesk and used as migration target |
| `KURAKA_MAIL_TLS_SKIP_VERIFY` | `false` | lab only: self-signed certificates |
| `KURAKA_DNS_RESOLVER` | `1.1.1.1:53` | resolver for public DNS checks |
| `KURAKA_MIGRATION_WORKERS` | `2` | concurrent migration jobs |

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
| `POST /helpdesks/{id}/agents` | superadmin only | `{mode:"self", password, role}` or `{mode:"other", name, email, role}` (Admin default or Agent); returns 201 `{email, url}` plus a generated `password` once; ready helpdesk required |
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
