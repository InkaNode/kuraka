# Phase 0: API mapping

Goal: know exactly which calls Kuraka makes to each system before writing code.

| System | Role in Kuraka | Doc | Verified live |
|---|---|---|---|
| Stalwart v1 (JMAP) | **Required.** Domains, alias domains, mailboxes, aliases, passwords, quotas, DKIM | [stalwart-api.md](stalwart-api.md) | Yes, all calls |
| Cloudflare API v4 | Optional DNS provider: publish records, secondary-domain redirects | [cloudflare-api.md](cloudflare-api.md) | Docs only (needs a token) |
| Libredesk v2.9 | Optional helpdesk: create email inboxes, agents, teams | [libredesk-api.md](libredesk-api.md) | Auth + inbox create |

## End-to-end flows proven in the lab

1. **Add domain**: `x:Domain/set create {name, aliases}` → DKIM keys generated → `dnsZoneFile` returns every record to publish.
2. **Alias domain**: `aliases: {"cliente1-alt.test": true}` → `juan@cliente1-alt.test` accepted and delivered to `juan@cliente1.test`.
3. **Mailbox + aliases**: `x:Account/set` with `aliases` → `contacto@`, `ventas@` accepted; unknown address rejected `550 5.1.1`.
4. **Password change**: replace `credentials` → old password 401, new 200.
5. **Connect support in one click**: create the mailbox in Stalwart → `POST /api/v1/inboxes` in Libredesk with `Authorization: token key:secret` → Libredesk logs `inbox initialized` and starts polling.
6. **Least-privilege service key**: domain destroy and network changes denied; account create/destroy allowed.

## Decisions carried into Phase 1

- Kuraka keeps Stalwart DNS management `Manual` and publishes DNS itself (Cloudflare plugin or copy-paste plus a verification loop).
- Multi-tenancy lives in Kuraka's DB (tenant to domain IDs); Stalwart's `Tenant` object is Enterprise-only and unused.
- Kuraka talks to Stalwart over the private Docker network with a dedicated service account key restricted by IP.
- One Libredesk instance per customer is supported by storing `{url, api_key, api_secret}` per tenant.

## Open questions

- Libredesk: does updating an inbox with masked passwords keep the stored ones? (Not checked in source.)
- Cloudflare: live test of the redirect ruleset once a token for inkanode.net exists.
- License: AGPL-3.0 or Apache-2.0 (needed before the first public push).
