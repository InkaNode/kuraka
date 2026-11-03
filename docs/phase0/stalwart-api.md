# Stalwart management API (v1.0.2)

Everything below was exercised against a live Stalwart `stalwartlabs/stalwart:v1` (1.0.2) on 2026-11-03.
Reference: https://stalw.art/docs/ref/ (object pages under `/docs/ref/object/<name>`).

## Transport

- Single endpoint: `POST /jmap/`, standard JMAP request envelope.
- `using`: `["urn:ietf:params:jmap:core", "urn:stalwart:jmap"]`.
- Admin objects live in the admin's account (`accountId` defaults to the caller's, `"b"` for the first admin). Kuraka can omit `accountId`.
- Methods follow JMAP: `x:<Object>/get`, `/set` (create, update, destroy), `/query`. Result references (`#ids` + `resultOf`) work, so a create plus a read is one round trip.
- Auth: `Authorization: Bearer <ApiKey secret>` (secret looks like `API_...`), or Basic for humans.
- Errors: method-level `["error", {"type": "forbidden", ...}]`, or per-object `notCreated`/`notUpdated`/`notDestroyed` maps. Without enough permissions the whole HTTP call can return `403` problem+json.

## Encoding quirks (important)

- **Lists are JSON objects with numeric string keys**: `{"0": {...}, "1": {...}}`. Applies to `credentials`, `aliases`.
- **Sets are objects of `name: true`**: `{"cliente1-alt.test": true}`. Applies to domain `aliases`, permission sets, DKIM `algorithms`.
- Patching a list entry (`"aliases/1": null`) **re-indexes** the list. Kuraka always sends the full list on update.
- Polymorphic values use `@type` (`"User"`, `"Group"`, `"Password"`, `"Automatic"`, `"Manual"`).

## Objects Kuraka uses

### Domain (`x:Domain/*`, perms `sysDomain{Get,Query,Create,Update,Destroy}`)

| Property | Notes |
|---|---|
| `name` | required |
| `aliases` | **alias domains** as a set. Mail to `x@alias` is delivered to `x@name`. Verified via SMTP RCPT. |
| `enable`, `description`, `catchAllAddress`, `subAddressing` | |
| `dkimManagement` | default `Automatic` (Ed25519 + RSA-SHA256). Keys appear a few seconds after create. Rotation needs automatic DNS management. |
| `dnsManagement` | default `Manual`. Kuraka keeps it `Manual` and publishes records itself. |
| `dnsZoneFile` | **server-set, the full set of records to publish**: SPF, MX, DMARC (`p=reject` by default), SRV (`_imaps`, `_submissions`, `_jmap`, `_caldavs`, `_carddavs`, `_pop3s`), MTA-STS, TLS-RPT, autoconfig/autodiscover CNAMEs, `ua-auto-config`, DKIM TXT. The RSA DKIM record is split across lines inside `( ... )`, so the parser must handle multi-line records. **Records for alias domains are not included**: Kuraka must derive them. |
| `reportAddressUri`, `allowRelaying` | |

Query filters: `text`, `name`, `memberTenantId` (Enterprise only).

### Account (`x:Account/*`, perms `sysAccount*`)

`@type: "User"` (mailbox) or `"Group"` (shared mailbox, no credentials).

| Property | Notes |
|---|---|
| `name` + `domainId` | required. `emailAddress` = `name@domain`, server-set. |
| `description` | display name |
| `credentials` | `{"0": {"@type": "Password", "secret": "..."}}`. Replacing it changes the password immediately (old one returns 401). |
| `aliases` | `{"0": {"name": "contacto", "domainId": "<id>", "description": null, "enable": true}}`, delivered to this account. Verified. |
| `quotas` | map, `maxDiskQuota` in bytes, plus `maxEmails`, `maxApiKeys`, etc.; `usedDiskQuota` is server-set |
| `roles` | `{"@type": "User"}`, `"Admin"`, or `{"@type": "Custom", "roleIds": {...}}` |
| `memberGroupIds`, `locale`, `timeZone` | |

No enabled/disabled flag on accounts. Suspension = rotate the password or disable via permissions (to define in Phase 1).
Query filters: `text`, `name`, `domainId`, `memberGroupIds`.

### DkimSignature (`x:DkimSignature/*`)

Read-only for Kuraka: `domainId`, `selector` (e.g. `v1-rsa-20261103`), `@type` (`Dkim1RsaSha256`, `Dkim1Ed25519Sha256`), `publicKey`, `stage`. Used to show DKIM status and to build alias-domain records.

### ApiKey (`x:ApiKey/*`)

- `create: {"description": "...", "permissions": {...}, "allowedIps": {...}, "expiresAt": ...}` returns `secret` **once**, in `created`.
- `permissions`: `{"@type": "Inherit"}`, `{"@type": "Replace", "permissions": {"<perm>": true}}`, or `{"@type": "Disable", "permissions": {...}}`.
- A key can only create objects that grant permissions **the key itself holds**. Creating a User grants the default user role (~246 permissions, including `sysApiKey*`), so a minimal `Replace` key **cannot create accounts**.
- Working service-key shape (verified): `Disable` of `sysDomainDestroy`, `sysNetworkListener{Create,Update,Destroy}`, `sysRole{Create,Update,Destroy}`. It creates and destroys users, groups and domains; domain destroy and listener changes are denied.
- `Replace` keys need at least `authenticate` + `jmapCoreEcho` to open a session.

## Gaps and decisions for Phase 1

1. **Service identity**: a `Disable` key inherited from the bootstrap admin can still mint API keys (escalation). Plan: a dedicated `kuraka-svc` account with a Custom role (user perms + `sysDomain*`/`sysAccount*`/`sysDkimSignature*`), key `Inherit`, `allowedIps` = the Kuraka container.
2. **Tenancy is Kuraka's job**: `Tenant`/`memberTenantId` are Enterprise. Kuraka maps tenant to domain IDs in its own DB and checks ownership on every call.
3. **Alias-domain DNS**: rewrite the primary's zone with the alias domain name; DKIM selectors are shared.
4. **DMARC default `p=reject`**: Kuraka should offer `p=none`, then `quarantine`, then `reject` for domains migrating from another provider.
5. **WebUI login is bound to the configured hostname** (OAuth client id). Admin access goes through `https://mail.<host>/admin`, never exposed beyond the admin network.
