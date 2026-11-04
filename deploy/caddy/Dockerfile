FROM caddy:2-builder AS build
RUN xcaddy build --with github.com/caddyserver/replace-response

FROM caddy:2
COPY --from=build /usr/bin/caddy /usr/bin/caddy
