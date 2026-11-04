# Architecture (Phase 1)

Kuraka is one Go binary with an embedded SQLite database. It sits on the private network next to Stalwart and is the only component exposed to tenant admins. Stalwart's admin interface is never public.

```
browser ──HTTPS──▶ reverse proxy ──▶ kuraka (:8080) ──JMAP (Bearer API key)──▶ stalwart (:8080, private)
                                         │
                                         └── SQLite (tenants, users, domains, sessions, audit)
```

## Roles

| Role | Scope |
|---|---|
| `superadmin` | Everything. Creates tenants, users, domains. Operated by the Kuraka owner (InkaNode). |
| `tenant_admin` | Only the domains of its tenant: mailboxes, aliases, passwords, quotas, alias domains, DNS status. |

## Packages

| Package | Responsibility |
|---|---|
| `cmd/kuraka` | Entry point: load config, open store, start HTTP server, print setup token. |
| `internal/version` | Release version. |
| `internal/config` | Env config (`KURAKA_*`). |
| `internal/auth` | argon2id password hashing, TOTP (RFC 6238), random tokens, AES-GCM secret box. |
| `internal/store` | SQLite (modernc, no cgo), embedded migrations, repositories. |
| `internal/stalwart` | Typed JMAP client: **only** the methods Kuraka needs (allowlist by construction, no generic passthrough). DNS zone file parser. |
| `internal/authz` | The single place that decides whether a principal may touch a domain or mailbox. |
| `internal/api` | HTTP handlers, sessions, CSRF/Origin checks, rate limiting, audit hooks. |

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `KURAKA_LISTEN` | `:8080` | HTTP listen address |
| `KURAKA_DB` | `kuraka.db` | SQLite path |
| `KURAKA_PUBLIC_URL` | required | e.g. `https://panel.inkanode.com`; used for Origin checks |
| `KURAKA_SECRET_KEY` | required | 64 hex chars (32 bytes); encrypts TOTP secrets |
| `KURAKA_STALWART_URL` | `http://stalwart:8080` | JMAP base URL |
| `KURAKA_STALWART_API_KEY` | required | Stalwart API key (`API_...`) for the Kuraka service account |
| `KURAKA_ALERT_EMAIL_FROM` | unset | Kuraka-managed mailbox that emails alerts to enabled superadmins; requires `KURAKA_MAIL_HOST` |
| `KURAKA_INSECURE_COOKIES` | `false` | Allow non-Secure cookies for local http dev |

## Data model (SQLite)

```
tenants   (id, name UNIQUE, created_at)
users     (id, tenant_id NULL→tenants, email UNIQUE, name, role, password_hash,
           totp_secret_enc NULL, totp_enabled, disabled, created_at, last_login_at NULL)
           CHECK role='superadmin' ⇔ tenant_id IS NULL
domains   (id, tenant_id→tenants, name UNIQUE, stalwart_id UNIQUE, created_at)
sessions  (id = sha256(token) hex, user_id→users, mfa_pending, created_at, expires_at, ip, user_agent)
audit_log (id, at, user_id NULL, tenant_id NULL, action, target, result, detail, ip)
```

Deleting a tenant is refused while it still owns domains.

## Authorization rules (`internal/authz`)

1. A `tenant_admin` may access a domain only if `domains.tenant_id` equals its tenant. Otherwise the result is **not found**, never "forbidden", so domain IDs cannot be probed.
2. A mailbox belongs to a domain when the Stalwart account's `domainId` equals that domain's `stalwart_id`. Mailbox routes load the account from Stalwart and check this before acting.
3. Every alias on a mailbox must point to a domain owned by the same tenant. This blocks the cross-tenant vector of adding `ceo@other-tenant.com` as an alias.
4. Alias domains (`Domain.aliases`) must not already exist as a domain in Stalwart or in Kuraka.
5. Only superadmins create, import or delete domains and manage tenants and users.

## HTTP API (`/api/v1`, JSON)

Errors: `{"error": {"code": "not_found", "message": "..."}}` with a matching HTTP status.

`GET /healthz` is served at the root, outside `/api/v1`, to anyone: 200 `{"status": "ok", "version", "stalwart": "ok"}`, or 503 with `"stalwart": "unreachable"` when Stalwart does not answer.

| Method and path | Who | Purpose |
|---|---|---|
| `POST /setup` | anyone with setup token | create the first superadmin; only while none exists |
| `POST /auth/login` | anyone | `{email, password}` → session cookie; `{"mfa_required": true}` when TOTP is on |
| `POST /auth/totp` | pending session | `{code}` completes login |
| `POST /auth/logout`, `GET /auth/me` | session | |
| `POST /auth/password` | session | `{current, new}` |
| `POST /auth/totp/enroll`, `POST /auth/totp/confirm`, `POST /auth/totp/disable` | session | TOTP management |
| `GET/POST /tenants`, `PATCH/DELETE /tenants/{id}` | superadmin | tenants |
| `GET/POST /users`, `PATCH /users/{id}` | superadmin | users (`role`, `tenant_id`, `disabled`, reset password) |
| `POST /tenants/{id}/domains` | superadmin | create in Stalwart `{name, aliases[]}` and assign |
| `POST /tenants/{id}/domains/import` | superadmin | link an existing Stalwart domain `{name}` |
| `DELETE /domains/{id}` | superadmin | unlink from Kuraka (Stalwart keeps the domain) |
| `GET /domains` | session | domains of the principal |
| `GET /domains/{id}` | session | name, alias domains, DKIM selectors, parsed DNS records (including derived alias-domain records) |
| `PUT /domains/{id}/aliases` | session | replace alias domains `{aliases[]}` |
| `GET /domains/{id}/mailboxes` | session | list users and groups of the domain |
| `POST /domains/{id}/mailboxes` | session | `{name, display_name, password?, quota_mb?, aliases[{name, domain_id}], shared?}`; a generated password is returned once |
| `PATCH /domains/{id}/mailboxes/{mid}` | session | display name, password, quota, aliases (full list) |
| `DELETE /domains/{id}/mailboxes/{mid}` | session | destroy |
| `GET /audit` | session | superadmin: all; tenant_admin: own tenant |

## Security baseline

- Passwords: argon2id (m=64 MiB, t=3, p=2, 16-byte salt, 32-byte key), PHC string format. Minimum length 12.
- Sessions: 32-byte random token in cookie `kuraka_session` (HttpOnly, Secure, SameSite=Strict, 12 h). Only the SHA-256 is stored.
- CSRF: mutating requests require `Content-Type: application/json` and an `Origin` header (or `Referer` when it is missing) that matches `KURAKA_PUBLIC_URL`.
- Login rate limits: 5 failures per IP+email in 15 minutes locks that pair for 15 minutes; 30 failed logins or TOTP codes per IP in 15 minutes locks that IP for 15 minutes across all emails. Successful authentication clears only the pair counter. Both in-memory limiters prune expired windows and cap storage at 100,000 keys, evicting the oldest window when full. Responses never reveal whether an email exists.
- TOTP secrets are encrypted at rest with AES-256-GCM using `KURAKA_SECRET_KEY`.
- Setup token: 32 random bytes, printed once to the log at startup while no superadmin exists; held in memory only.
- Every mutating action writes an audit row, including denied attempts.
