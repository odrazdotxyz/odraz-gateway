# Architecture

## Why a gateway

A browser should not hold a storage server's credentials, cannot talk to every vendor's different API, and cannot
call servers that do not allow cross-origin requests. The gateway is the small server in between. Desktop and mobile
apps do not need it: they embed the Odraz Go SDK, which is what the gateway itself uses for everything.

```
  ┌────────────┐   cookie + JSON    ┌───────────────────────────────┐   SDK    ┌────────────┐
  │  web UI    │ ─────────────────▶ │ odraz-gateway                 │ ───────▶ │ OpenCloud  │
  │ (any)      │ ◀───────────────── │  server.go   routes, CSP      │          └────────────┘
  └────────────┘                    │  auth.go     sign-in, sessions│   SDK    ┌────────────┐
        │                           │  api.go      files API        │ ───────▶ │ Nextcloud  │
        │ iframe                    │  photos.go   photos + manifest│          └────────────┘
        ▼                           │  zip.go      streamed zips    │
  ┌────────────┐  file + callbacks  │  editor.go   editor host      │   Store  ┌────────────┐
  │ ONLYOFFICE │ ◀────────────────▶ │  store.go    session store    │ ───────▶ │ file/memory│
  │ (separate) │                    │  ops.go      logs, limits     │          └────────────┘
  └────────────┘                    └───────────────────────────────┘
```

## Pieces

| Path | Role |
|---|---|
| `cmd/odraz-gateway` | the product binary: flags and environment, nothing else |
| `pkg/gateway` | the server as an importable package: `Config`, `New`, `Run`, `Handler` |
| `pkg/manifest` | reads and writes `/.odraz/manifest.json` in a drive ([manifest.md](manifest.md)) |
| `internal/editor` | the document editor provider: JWT, formats, SSRF-safe client (no storage code) |
| `api/openapi.yaml` | the contract; `api/api.go` embeds it for tests |
| `examples/vanilla` | the reference UI (plain JS, no build) and its embedding as `vanilla.FS()` |

## Requests

Every route is a plain `net/http` handler wrapped by `guard` (request id, access log, rate limit). Authenticated
routes are wrapped by `authed` (needs a session with at least one connected server) and state-changing ones by `csrf`
(the `X-Requested-With: odraz` header). Every file call names the server (`acct`) and the drive; the gateway keeps one
open `files.FS` per drive per session.

## Sessions and the Store

The browser holds one opaque 256-bit id in an HttpOnly cookie. Everything else lives on the server, in a **Store**
(`store.go`):

* which servers the session is connected to, and their tokens (the SDK's secret store, scoped to the session),
* a sign-in in progress (OIDC state and PKCE verifier),
* the user's own editor setting.

`NewMemoryStore` is for development. `NewFileStore` writes one file, encrypted and authenticated with AES-256-GCM
under a key you provide, atomically, mode 0600; a wrong key or a changed file is refused at start-up. Live
connections (HTTP clients, open drives) are a cache in memory, rebuilt from the Store on the first request after a
restart. The `Store` interface is small on purpose: a Redis or Postgres implementation for several gateway instances
would implement the same five methods. **v1 is single-instance.**

Sessions last 12 hours from creation (not sliding). A browser that is not signed in gets a **new** session id at
the moment it signs in, so an id known beforehand never becomes a signed-in session (fixation).

## Several backends

`Config.Backends` lists the servers. Each has an id, a vendor adapter (`opencloud`, `nextcloud`), a URL and its own
TLS setting. A session can connect to any of them, independently; disconnecting the last one ends the session. A copy
or move across servers streams the bytes through the gateway.

## The demo-login hook

`Config.DemoLogin` is a function the **embedding program** may set to offer a one-click sign-in with a fixed
credential. Without it the route `POST /auth/demo` is not registered at all. The product binary never sets it; the
Odraz demo does (it lives in the demo repository). See [security.md](security.md).

## The document editor

A separate service (ONLYOFFICE Document Server) that the gateway talks to over HTTP: it downloads files from the
gateway and reports saves back, and the gateway reads and writes through the SDK. See [editor.md](editor.md).

## What is deliberately not here

No vendor administration (users, groups, quotas), no sharing management, no search, no multi-instance sessions yet,
no database. Contacts and calendar will arrive as new SDK domains (CardDAV, CalDAV) and new route groups.
