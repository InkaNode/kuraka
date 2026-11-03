# Libredesk HTTP API for an external admin panel

Source: github.com/abhinavxd/libredesk, branch `main` (v2.9.x). Responses use an envelope: `{"data": ...}` on success.

## 1. API-key auth
Source: `cmd/middlewares.go` (`authenticateUser`), `cmd/users.go` (`handleGenerateAPIKey`), `cmd/handlers.go`.

- `authenticateUser` calls `r.ParseAuthHeader(fastglue.AuthBasic | fastglue.AuthToken)`. Both forms are accepted:
  - `Authorization: token <api_key>:<api_secret>`
  - `Authorization: Basic base64(<api_key>:<api_secret>)`
  - The exact `token` syntax comes from fastglue, not from this repo. Verify it against a live instance.
- API-key requests skip CSRF.
- Create a key with `POST /api/v1/agents/{id}/api-key`. It needs `users:manage` and has no body.
  - It returns `{"api_key": "...", "api_secret": "..."}`. The secret is shown once and stored hashed (bcrypt or a hash upgraded on use).
  - The target must be a user of type `agent`.
  - `DELETE /api/v1/agents/{id}/api-key` revokes it.
- A key acts as its agent, with that agent's role permissions. The admin panel needs an agent whose role holds `inboxes:manage`, `users:manage`, `teams:manage` and `roles:manage`. The default Admin role has these.

## 2. Endpoints
Source: `cmd/handlers.go` (route table), `cmd/inboxes.go`, `cmd/users.go`, `cmd/teams.go`, `internal/inbox/models/models.go`, `internal/team/models/models.go`.

| Method | Path | Permission | Body (JSON tags) |
|---|---|---|---|
| GET | /api/v1/inboxes | auth only | none |
| GET | /api/v1/inboxes/{id} | inboxes:manage | none |
| POST | /api/v1/inboxes | inboxes:manage | `name`, `channel` ("email"), `enabled`, `csat_enabled`, `prompt_tags_on_reply`, `reopen_window_hours`, `from`, `from_name_template`, `aliases[]{email}`, `linked_email_inbox_id`, `secret` (livechat), `config` (object) |
| PUT | /api/v1/inboxes/{id} | inboxes:manage | same fields as POST |
| PUT | /api/v1/inboxes/{id}/toggle | inboxes:manage | none; flips `enabled` |
| DELETE | /api/v1/inboxes/{id} | inboxes:manage | none; soft delete |
| POST | /api/v1/inboxes/{id}/aliases/verify | inboxes:manage | `email`; returns `{"status":"pending"}` |
| GET | /api/v1/agents | users:manage | none |
| GET | /api/v1/agents/{id} | users:manage | none |
| POST | /api/v1/agents | users:manage | `first_name`, `last_name`, `email`, `send_welcome_email`, `teams[]` (names), `roles[]` (names), `enabled`, `availability_status`, `new_password` |
| PUT | /api/v1/agents/{id} | users:manage | same fields as POST |
| DELETE | /api/v1/agents/{id} | users:manage | none |
| GET | /api/v1/teams | teams:manage | none |
| GET | /api/v1/teams/{id} | teams:manage | none |
| POST | /api/v1/teams | teams:manage | `name`, `emoji`, `timezone`, `conversation_assignment_type`, `business_hours_id`, `sla_policy_id`, `max_auto_assigned_conversations` |
| PUT | /api/v1/teams/{id} | teams:manage | same fields as POST |
| DELETE | /api/v1/teams/{id} | teams:manage | none |
| GET | /api/v1/roles | auth only | none |
| GET | /api/v1/roles/{id} | roles:manage | none |

Notes:
- GET inbox responses mask every password and OAuth secret with the dummy string (`stringutil.PasswordDummy` repeated 10 times). The update path restores masked secrets. This is confirmed in the source for WhatsApp (`MergeWhatsAppSecrets`) and not checked for email. Re-send real passwords if unsure.
- Create and update validation in `validateInbox` requires `config`, `name` and `channel`.
- `agentReq` takes `teams` and `roles` as name strings.
- Other roles routes (POST, PUT, DELETE) need `roles:manage`.

## 3. Email inbox `config`
Source: `internal/inbox/models/models.go` (`Config`, `SMTPConfig`, `IMAPConfig`, `OAuthConfig`); validation in `cmd/inboxes.go` (`validateEmailConfig`).

Top level:

| Field | Notes |
|---|---|
| `auth_type` | `"password"` or `"oauth2"`; empty is accepted |
| `oauth` | object or null; see below |
| `smtp[]`, `imap[]` | arrays |
| `from` | the address; the inbox-level `from` is separate |
| `from_name_template` | string |
| `reply_to` | must parse as an email address |
| `enable_plus_addressing` | bool |

`smtp[]` entries:

| Field | Notes |
|---|---|
| `host` | required |
| `port` | int, must be > 0 |
| `username`, `password` | strings |
| `auth_protocol` | `""`, `none`, `plain`, `login`, `cram` (checked for password auth) |
| `tls_type` | string; the allowed values are not checked for SMTP in the validator. Use `none`, `starttls` or `tls`, the same set as IMAP, and confirm against smtppool. |
| `tls_skip_verify` | bool |
| `hello_hostname` | string |
| `max_conns` | int |
| `max_msg_retries` | json `max_msg_retries` |
| `idle_timeout` | duration string, e.g. `"25s"` |
| `pool_wait_timeout` | duration string. The tag is `pool_wait_timeout`, not `wait_timeout`. |
| `ssl` | bool |

`imap[]` entries:

| Field | Notes |
|---|---|
| `host` | required |
| `port` | must be > 0 |
| `username`, `password` | strings |
| `mailbox` | required, e.g. `"INBOX"` |
| `read_interval` | duration string, e.g. `"5m"` |
| `scan_inbox_since` | string; duration, e.g. `"48h"` (format not verified) |
| `tls_type` | `none`, `starttls` or `tls`; required and validated |
| `tls_skip_verify` | bool |


`oauth` fields: `provider` (`google` or `microsoft`), `access_token`, `refresh_token`, `expires_at` (RFC3339), `client_id` (required), `client_secret`, `tenant_id`. OAuth inboxes are normally authorised through `/api/v1/inboxes/oauth/{provider}/authorize` and `/callback`, which is a browser flow.

Example POST /api/v1/inboxes:
```json
{
  "name": "Support",
  "channel": "email",
  "enabled": true,
  "csat_enabled": false,
  "from": "Support <support@example.com>",
  "config": {
    "auth_type": "password",
    "from": "Support <support@example.com>",
    "reply_to": "",
    "enable_plus_addressing": false,
    "smtp": [{
      "host": "mail.example.com", "port": 587,
      "username": "support@example.com", "password": "secret",
      "auth_protocol": "login", "tls_type": "starttls", "tls_skip_verify": false,
      "hello_hostname": "", "max_conns": 5, "max_msg_retries": 3,
      "idle_timeout": "25s", "pool_wait_timeout": "5s"
    }],
    "imap": [{
      "host": "mail.example.com", "port": 993,
      "username": "support@example.com", "password": "secret",
      "mailbox": "INBOX", "tls_type": "tls", "tls_skip_verify": false,
      "read_interval": "5m", "scan_inbox_since": "48h"
    }]
  }
}
```

## 4. Not possible via the API
Source: `cmd/handlers.go`, `cmd/inboxes.go`. Items marked "unverified" were not checked in source.

- The first admin and the system user are created outside the API. Libredesk's install step does this (`--install`, env password). Unverified here.
- The initial API key needs a prior session login or the first admin. Bootstrap by logging in once, then calling `POST /api/v1/agents/{id}/api-key`.
- General settings (`PUT /api/v1/settings/general`) need `general_settings:manage`. Notification and email settings use their own permissions. Server-level config (DB, listen address) is not exposed.
- OAuth inbox authorisation needs a browser redirect.
- The system user cannot get an API key (`type` must be `agent`).
- Passwords are never returned, so secrets cannot be read back.
