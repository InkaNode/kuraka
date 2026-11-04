# Security policy

## Supported versions

| Version | Supported |
|---|---|
| 0.1.x | yes |
| older | no |

## Reporting a vulnerability

Report privately through GitHub Security Advisories:
<https://github.com/InkaNode/kuraka/security/advisories/new>

Do not open a public issue or pull request for a vulnerability. Include the affected version, steps to reproduce and the impact you observed.

## Scope

In scope:

- The Kuraka binary, API and web panel: authentication, sessions, 2FA, CSRF, tenant isolation, audit, secret storage.
- The Stalwart, Cloudflare and Libredesk integrations as implemented in this repository.
- The deployment bundle in `deploy/` and the Libredesk bundles Kuraka generates.

Out of scope: vulnerabilities in Stalwart, Libredesk, Caddy, Cloudflare or Docker themselves (report them upstream), and issues that require an already compromised host or a leaked `deploy/.env`.

## What to expect

- Acknowledgement within 3 business days.
- An assessment and planned fix timeline within 10 business days.
- A fix released in a 0.1.x patch, with a GitHub advisory and credit if you want it.

Please give us a reasonable time to release a fix before any public disclosure.
