# Deployment bundle

Single-VPS stack: Caddy (HTTPS), Stalwart (mail) and Kuraka (panel).

```sh
cp deploy/.env.example deploy/.env   # edit the first block
bash deploy/setup.sh
```

Full guide: [docs/install.md](../docs/install.md).

Stalwart uses the [official web interface](https://github.com/stalwartlabs/webui).
Kuraka's addon hides the views Kuraka manages. Caddy injects
`/_kuraka/addon/<app>.css` and `/_kuraka/addon/<app>.js` into HTML responses
(`stalwart` for mail, `desk` for Libredesk), and proxies those asset paths to
Kuraka. The `kuraka_addon_proxy` snippet takes the app name and upstream and
requests uncompressed responses with `Accept-Encoding: identity`. It preserves
the public 404 for Stalwart's `/admin*` and `/login*` paths and adds no CSP.

The custom Caddy image, `kuraka-caddy:local`, is built locally from
`deploy/caddy/Dockerfile` with `github.com/caddyserver/replace-response`.
Its `replace` directive uses `order replace after encode` and a
`Content-Type: text/html*` response matcher. Replacement is buffered (no
`stream`). The module replaces every literal `</head>` and has no
first-match-only option; application HTML must contain a single such marker.

Re-run `bash deploy/setup.sh` from the repository root to switch an existing
install to the official web interface and rebuild Caddy. Setup updates and
logs `resourceUrl` when it differs, including installs using the previous web
interface URL. Generated `desk.caddy` files import
`kuraka_addon_proxy desk kuraka-<name>-desk:9000` alongside `security_headers`.

## Updating Kuraka

Use the update button on the **Sistema** page. Kuraka writes the release tag
and commit to `deploy/updates/kuraka.json`; root's `kuraka-updates.timer`
checks every minute and applies it without giving Kuraka the Docker socket.
The commit must be on `origin/main`.

The timer stops Kuraka, backs up its data to `deploy/backups/kuraka-<UTC timestamp>.tar`,
checks out the commit and sets `KURAKA_VERSION=sha-<commit>` in `deploy/.env`.
It pulls `ghcr.io/inkanode/kuraka:sha-<commit>` or builds it locally with
`KURAKA_COMMIT` if the pull fails, starts the stack with `up -d --build` to
rebuild Caddy when its Dockerfile changes, reloads Caddy and waits up
to 180 seconds for Kuraka to become healthy. On failure it rolls back the
checkout and version, restores the data backup if the new container may have
started, and restarts the stack with `up -d --build`. Results go to `deploy/updates/kuraka.result.json`;
failed recovery steps are logged and included in the result for manual recovery.
The GHCR package `ghcr.io/inkanode/kuraka` must be public for pulls.

While Kuraka is down, Caddy serves `deploy/maintenance/index.html` if present,
otherwise `deploy/maintenance.html`, with HTTP 503, `Retry-After: 30` and
`Cache-Control: no-store`. Branding customizes the page by writing
`deploy/maintenance/index.html`. The default page refreshes every 20 seconds;
email keeps working. Setup gives `deploy/maintenance/` to uid/gid `65532:65532`
with mode `755`. This directory, `.env`, update requests and backups are ignored
by Git so checkout updates preserve them.
