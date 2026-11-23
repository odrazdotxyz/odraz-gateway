# Deploying

One container, one volume, a reverse proxy for TLS. v1 runs as **one instance** (sessions are in one file).

## The image

`ghcr.io/odrazdotxyz/odraz-gateway:<version>` (published from release tags): about 9 MB, a single static binary on
`distroless/static`, runs as user 65532 with no shell, `HEALTHCHECK` built in, listens on `0.0.0.0:8080`, keeps its
session file under `/data`. Build it yourself with `docker build -t odraz-gateway .`.

## A complete example

`compose.yaml`, with Caddy for automatic HTTPS (any proxy works; see below):

```yaml
services:
  gateway:
    image: ghcr.io/odrazdotxyz/odraz-gateway:0.1.0
    restart: unless-stopped
    environment:
      ODRAZ_PUBLIC_URL: https://files.example.com
      ODRAZ_TRUST_PROXY: "true"
      ODRAZ_BACKENDS: >-
        id=work,vendor=opencloud,url=https://cloud.example.com;
        id=home,vendor=nextcloud,url=https://nc.example.com
      ODRAZ_SESSION_FILE: /data/sessions.bin
      ODRAZ_SESSION_KEY: ${ODRAZ_SESSION_KEY:?generate one with: openssl rand -base64 32}
      # optional document editor, run by you (see editor.md):
      # ODRAZ_OFFICE_URL: https://office.example.com
      # ODRAZ_OFFICE_SECRET: ${OFFICE_SECRET}
    volumes: [gateway-data:/data]
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    # no published port: only the proxy reaches it
  caddy:
    image: caddy:2
    restart: unless-stopped
    ports: ["80:80", "443:443"]
    command: caddy reverse-proxy --from files.example.com --to gateway:8080
    volumes: [caddy-data:/data]
volumes: {gateway-data: {}, caddy-data: {}}
```

Put the key in a `.env` file next to it (mode 600, not in git): `ODRAZ_SESSION_KEY=$(openssl rand -base64 32)`.

## Behind your own reverse proxy

* Terminate TLS there and forward to the gateway. **Set `ODRAZ_PUBLIC_URL` to the public https address** (cookie
  `Secure`, OIDC redirect) and `ODRAZ_TRUST_PROXY=true` (rate limits and logs use the real client address; only
  correct if nothing else can reach the gateway).
* Forward `Host`, and let large uploads and downloads stream: turn off request and response buffering for the gateway's
  location (nginx: `proxy_request_buffering off; proxy_buffering off; client_max_body_size 0;`; the gateway has its own
  limit, `-max-upload-bytes`) and use generous read timeouts.
* Add what the gateway leaves to you: `Strict-Transport-Security`.
* Serve the UI and the API from **the same origin**. Do not enable CORS.

## OIDC (browser sign-in for OpenCloud)

Register the gateway as a public OIDC client in the identity provider OpenCloud trusts: client id as `ODRAZ_CLIENT_ID`
(default `odraz-gateway`), redirect URI `https://files.example.com/auth/callback`, PKCE. Without it users sign in with
app tokens or passwords.

## Backups and upgrades

The only state is the session file. You can lose it (users sign in again). Back up nothing else; the files stay on
your storage servers (the Photos folder choice is a file, `/.odraz/manifest.json`, in each drive). To upgrade, pull the
new image and restart: sessions survive (same key); an older gateway cannot read a newer file, so keep the version
when you roll back.

## Sizing

The gateway is small: it holds sessions and streams bytes. Measured idle: about 10 MB resident. Memory grows with the number
of concurrent sessions (a few KB each) and with open transfers (streamed, not buffered). CPU is mostly TLS (at the
proxy) and zip compression. A 1 vCPU / 256 MB host is enough for dozens of users. The bandwidth between the gateway
and your storage servers is the real limit, and copying or moving across servers goes through it twice.

The **document editor is a different matter**: ONLYOFFICE Document Server wants about 4 GB of RAM for itself (vendor
figure), and measured here it used about 340 MiB idle and peaked at about 1.2 GiB with a few documents. It does not
run on a 500 MB host. The gateway only needs its URL and secret, so run the editor elsewhere. See [editor.md](editor.md).

## Checks before going live

* `curl https://files.example.com/healthz` answers `{"ok":true}`; the response has `X-Request-Id`.
* The cookie is `Secure` (browser dev tools) and a plain-http request does not set one.
* `POST /auth/demo` is `404`/`405` (there is no demo login in this binary).
* The log is JSON and has no query strings.
* The container runs as non-root: `docker inspect --format '{{.Config.User}}'`.
