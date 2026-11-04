# Deployment bundle

Single-VPS stack: Caddy (HTTPS), Stalwart (mail) and Kuraka (panel).

```sh
cp deploy/.env.example deploy/.env   # edit the first block
bash deploy/setup.sh
```

Full guide: [docs/install.md](../docs/install.md).

## Updating Kuraka

Use the update button on the **Sistema** page. Kuraka writes the release tag
and commit to `deploy/updates/kuraka.json`; root's `kuraka-updates.timer`
checks every minute and applies it without giving Kuraka the Docker socket.
The commit must be on `origin/main`.

The timer stops Kuraka, backs up its data to `deploy/backups/kuraka-<UTC timestamp>.tar`,
checks out the commit and sets `KURAKA_VERSION=sha-<commit>` in `deploy/.env`.
It pulls `ghcr.io/inkanode/kuraka:sha-<commit>` or builds it locally with
`KURAKA_COMMIT` if the pull fails, starts the stack, reloads Caddy and waits up
to 180 seconds for Kuraka to become healthy. On failure it rolls back the
checkout and version, restores the data backup if the new container may have
started, and restarts the stack. Results go to `deploy/updates/kuraka.result.json`;
failed recovery steps are logged and included in the result for manual recovery.
The GHCR package `ghcr.io/inkanode/kuraka` must be public for pulls.

While Kuraka is down, Caddy serves `deploy/maintenance/index.html` if present,
otherwise `deploy/maintenance.html`, with HTTP 503, `Retry-After: 30` and
`Cache-Control: no-store`. Branding customizes the page by writing
`deploy/maintenance/index.html`. The default page refreshes every 20 seconds;
email keeps working. Setup gives `deploy/maintenance/` to uid/gid `65532:65532`
with mode `755`. This directory, `.env`, update requests and backups are ignored
by Git so checkout updates preserve them.
