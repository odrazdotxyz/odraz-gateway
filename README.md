# Odraz gateway

A small server that lets a **web app** use files stored on **self-hosted clouds**, whichever they are. It signs the
user in to their servers (OpenCloud, Nextcloud, ...), keeps their tokens on the server side, and offers one JSON API
for drives, files, thumbnails, zip downloads, photos and a document editor, the same for every vendor.

```
 browser ──cookie──▶  odraz-gateway  ──Odraz Go SDK──▶  OpenCloud
 (any web UI)         • sessions, CSRF                   Nextcloud
                      • one API: /api/v1                 (WebDAV, ...)
                      • serves the UI                         ▲
 document editor  ◀──────────────────── file bytes ───────────┘
 (ONLYOFFICE, yours)
```

Desktop and mobile apps embed the [Go SDK](https://github.com/odrazdotxyz/odraz-go-sdk) directly. The gateway exists
for **web UIs**: a browser cannot hold a server's credentials safely, and the vendors' own APIs differ. The API is the
product: [`api/openapi.yaml`](api/openapi.yaml) is the contract, and anyone can write a UI against it. A small reference
UI with no build step is in [`examples/vanilla`](examples/vanilla).

## Quick start

You need Go 1.23+ and a server to connect to (an OpenCloud or Nextcloud you can sign in to with an app token or app
password).

```sh
git clone https://github.com/odrazdotxyz/odraz-gateway && cd odraz-gateway
go run ./cmd/odraz-gateway -backend 'vendor=nextcloud,url=https://nc.example.com'
# open http://localhost:8080, choose the server, sign in with your user name and an app password
```

* **OpenCloud** takes an app token (an administrator creates one with `opencloud auth-app create --user-name=NAME
  --expiration=...`; the auth-app service and `PROXY_ENABLE_APP_AUTH` must be on), or browser sign-in through the
  identity provider it trusts (OIDC, see [docs/configuration.md](docs/configuration.md#oidc-sign-in)).
* **Nextcloud** takes an app password (Settings → Security → Devices & sessions).
* A server with a self-signed certificate needs `insecure=true` **on that backend**, for example
  `-backend 'vendor=opencloud,url=https://localhost:9200,insecure=true'`.

With Docker:

```sh
docker run --rm -p 8080:8080 -e ODRAZ_PUBLIC_URL=http://localhost:8080 \
  -e ODRAZ_BACKENDS='vendor=nextcloud,url=https://nc.example.com' ghcr.io/odrazdotxyz/odraz-gateway:latest
```

By default sessions are kept in memory: restarting signs everyone out. For real use keep them in an encrypted file
(`-session-file`, `ODRAZ_SESSION_KEY`) and put the gateway behind HTTPS: [docs/deploying.md](docs/deploying.md).

## What it does

* **Several servers at once.** A user connects any of the configured servers independently; the API merges them. Files
  can be copied and moved between servers (streamed through the gateway).
* **Drives and files:** list, download with ranges, upload, create folders, rename/move, copy, delete, selection as a zip,
  server-rendered thumbnails, the user's avatar, quotas.
* **Photos:** a folder per drive chosen by the user and remembered in the drive itself
  ([docs/manifest.md](docs/manifest.md)).
* **Documents:** open `.docx .xlsx .pptx .odt .ods .odp .txt .csv` (and view `.pdf`) from any server in one
  ONLYOFFICE editor you run yourself, saved back to where they came from ([docs/editor.md](docs/editor.md)).
* **Operations:** structured logs with request ids, `/healthz`, rate limits, upload limits, graceful shutdown, a 9 MB
  non-root container image.

## Documentation

| | |
|---|---|
| [docs/architecture.md](docs/architecture.md) | how it is built, and why |
| [docs/api.md](docs/api.md), [api/openapi.yaml](api/openapi.yaml) | the HTTP API |
| [docs/configuration.md](docs/configuration.md) | every flag and variable, with defaults |
| [docs/security.md](docs/security.md) | threat model: sessions, CSRF, CSP, SSRF, secrets |
| [docs/deploying.md](docs/deploying.md) | Docker, reverse proxy with TLS, an example compose, sizing |
| [docs/building-a-ui.md](docs/building-a-ui.md) | write your own UI, or swap the reference one |
| [docs/editor.md](docs/editor.md) | the document editor |
| [docs/manifest.md](docs/manifest.md) | the `.odraz/manifest.json` convention |
| [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md), [CHANGELOG.md](CHANGELOG.md) | |

## As a library

`pkg/gateway` is importable: build the same server into your own program, with your own login hook or UI.

```go
srv := gateway.New(gateway.Config{
    PublicURL: "https://files.example.com",
    Backends:  []gateway.Backend{{ID: "nc", Vendor: "nextcloud", URL: "https://nc.example.com"}},
    WebFS:     myUI, // an fs.FS, or WebDir for a folder
})
log.Fatal(srv.Run(ctx))
```

## Licence

[GNU AGPL-3.0](LICENSE). If you run a modified gateway as a network service, the AGPL requires you to offer its source
to its users. The SDK it builds on is Apache-2.0. Third-party notices: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
The logos in `examples/vanilla/assets` are Odraz brand assets under CC BY-ND 4.0: use them unmodified, with attribution.
