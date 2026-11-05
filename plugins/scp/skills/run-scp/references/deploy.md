# Deployment recipes

Replace `<version>` with the release (for example `2.1.0`, without the `v`) and `scp.example.org` with the portal's address. The
release's `docs/production.md` and the operator guide's section 3 are the reference.

## One host: Docker Compose and Caddy (the default)

Caddy gets and renews the certificate, passes `X-Forwarded-For` and streams the server-sent events without buffering.

`compose.yaml`:

```yaml
services:
  scp:
    image: ghcr.io/cantara/source-code-portal:<version>
    command: [serve, --config, /etc/scp/scp.yaml]
    restart: unless-stopped
    read_only: true
    cap_drop: [ALL]
    volumes:
      - scp-data:/data
      - ./scp.yaml:/etc/scp/scp.yaml:ro
      - ./secrets:/run/secrets:ro
    ports: ["127.0.0.1:9091:9091"]       # admin: health and metrics, on the host only
    networks: [edge]
  caddy:
    image: caddy:2
    restart: unless-stopped
    ports: ["80:80", "443:443"]
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
    networks: [edge]
networks:
  edge:
    ipam: { config: [{ subnet: 172.30.0.0/24 }] }   # this is server.trusted_proxies
volumes:
  scp-data:
  caddy-data:
```

`Caddyfile`:

```
scp.example.org {
	encode zstd gzip
	reverse_proxy scp:9090
}
```

Start with `docker compose up -d && docker compose logs -f scp`. Upgrade: change the tag, `docker compose up -d`.

## Kubernetes

One replica with a persistent volume (the cache is one SQLite file):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: scp }
spec:
  replicas: 1
  strategy: { type: Recreate }
  selector: { matchLabels: { app: scp } }
  template:
    metadata: { labels: { app: scp } }
    spec:
      securityContext: { runAsNonRoot: true, fsGroup: 65532, seccompProfile: { type: RuntimeDefault } }
      containers:
        - name: scp
          image: ghcr.io/cantara/source-code-portal:<version>
          args: [serve, --config, /etc/scp/scp.yaml]
          ports: [{ name: public, containerPort: 9090 }, { name: admin, containerPort: 9091 }]
          securityContext: { readOnlyRootFilesystem: true, allowPrivilegeEscalation: false, capabilities: { drop: [ALL] } }
          startupProbe: { httpGet: { path: /readyz, port: admin }, periodSeconds: 10, failureThreshold: 90 }
          readinessProbe: { httpGet: { path: /readyz, port: admin }, periodSeconds: 10 }
          livenessProbe: { httpGet: { path: /livez, port: admin }, periodSeconds: 20, failureThreshold: 3 }
          resources: { requests: { cpu: 100m, memory: 256Mi }, limits: { memory: 768Mi } }
          volumeMounts:
            - { name: data, mountPath: /data }
            - { name: config, mountPath: /etc/scp, readOnly: true }
            - { name: secrets, mountPath: /run/secrets, readOnly: true }
      volumes:
        - { name: data, persistentVolumeClaim: { claimName: scp-data } }
        - { name: config, configMap: { name: scp-config } }
        - { name: secrets, secret: { secretName: scp-secrets } }
```

Expose only `public` through the ingress: it must pass `X-Forwarded-For` from an address in `server.trusted_proxies`, accept 25 MiB
bodies on `/webhooks/github`, and pass `/api/v1/events` unbuffered with a read timeout above 30 seconds. Several instances: add
`cluster.addresses` (Hazelcast 5), drop the claim and `Recreate`, an `emptyDir` cache, and set `replicas`.

## A binary under systemd

```sh
sha256sum -c scp_<version>_checksums.txt --ignore-missing
tar xzf scp_<version>_linux_amd64.tar.gz && tar xzf scp-web_<version>.tar.gz   # the bundle unpacks into bundle/
```

`/etc/systemd/system/scp.service` (Node.js 24 on the `PATH`, or `web.node`):

```ini
[Unit]
Description=Source Code Portal
After=network-online.target
Wants=network-online.target

[Service]
User=scp
Environment=SCP_WEB_DIR=/opt/scp/bundle
ExecStart=/opt/scp/scp serve --config /etc/scp/scp.yaml
Restart=on-failure
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/scp
StateDirectory=scp

[Install]
WantedBy=multi-user.target
```

Set `store.path: /var/lib/scp/scp.db` and put a reverse proxy with TLS in front of `:9090`. Upgrade: replace the binary and the
bundle together, restart.

## Verify the image

```sh
cosign verify ghcr.io/cantara/source-code-portal:<version> \
  --certificate-identity https://github.com/Cantara/source-code-portal/.github/workflows/release.yml@refs/tags/v<version> \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Agents (MCP)

`/mcp` serves the portal's read-only tools to MCP clients; nothing to configure. Check it after a deploy and give a team's agents a
token from `auth.tokens` (its `groups` in `access.private_groups` for the private view):

```sh
curl -s https://portal.example.org/api/v1/tools | head -c 300      # the tools agents get
claude mcp add --transport http scp https://portal.example.org/mcp --header "Authorization: Bearer $SCP_TOKEN"
```

`/mcp` refuses an `Origin` other than `server.public_url`'s: set the public URL when the proxy serves another host name.

## A screen on a wall

`/present` is the presentation page for a tablet or a TV: one screen, no scrolling, live, nothing to configure. Bookmark one per team
(`/present?groups=payments,identity`, `?work=hand,ai`); on an iPad add it to the home screen. The page picks its profile from the screen (*Handheld portrait*, *Handheld
landscape* up to 1600 px, *Desk* beyond, the commit log at 60%), each with its own commit column; a TV is told it is on a wall once
with `/present?profile=wall`. Behind `access.require_auth` sign in
once on the device, with single sign-on or a token of its own from `auth.tokens` (only the groups it shows): the page's hourly reload
renews the session. An administrator arranges it like the dashboard; with `layout.dir`
the layout is `present.json` beside `dashboard.json`.

## Search

The search (`/search`, the header's box, `GET /api/v1/search?q=`) needs nothing configured: it is an index in memory of what the
portal already reads, rebuilt as repositories change, never stored and never asked of GitHub. Check it after a deploy:

```sh
curl -s 'https://portal.example.org/api/v1/search?q=readme' | jq .counts    # hits by kind, from the public view
curl -s http://127.0.0.1:9091/api/v1/status | jq .search                    # on the host: segments, terms, bytes, build time
```

`scp_search_requests_total` and `scp_search_duration_seconds` (by `op`: `search`, `suggest`) are on `/metrics`.
