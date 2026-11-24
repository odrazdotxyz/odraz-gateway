# Configuration

Every setting is a flag. Most also have an environment variable (prefix `ODRAZ_`), which is the better place for
anything secret because flags show up in the process list. **A flag wins over its variable.**

```
odraz-gateway [flags]
```

## Backends

`-backend` (repeat for each server), or `ODRAZ_BACKENDS` with entries separated by semicolons.

```
-backend 'id=NAME,vendor=opencloud|nextcloud,url=URL[,insecure=true]'
```

| Key | Required | |
|---|---|---|
| `vendor` | yes | `opencloud` or `nextcloud` |
| `url` | yes | `http://` or `https://`, no trailing slash needed |
| `id` | no | unique name used in the API and the UI; default: the vendor. Two servers of one vendor need distinct ids |
| `insecure` | no | `true` accepts **this server's** self-signed certificate. Default `false`. There is no global switch |

At least one backend is required. Example: `ODRAZ_BACKENDS='id=work,vendor=opencloud,url=https://cloud.example.com;id=home,vendor=nextcloud,url=https://nc.example.com'`

## Server

| Flag | Variable | Default | |
|---|---|---|---|
| `-listen` | `ODRAZ_LISTEN` | `127.0.0.1:8080` (the container image sets `0.0.0.0:8080`) | address to listen on |
| `-public-url` | `ODRAZ_PUBLIC_URL` | `http://localhost:8080` | the URL browsers use. **Use `https://` in production**: the session cookie is then marked `Secure`. Also the base of the OIDC redirect |
| `-trust-proxy` | `ODRAZ_TRUST_PROXY` | off | behind a reverse proxy: take the client address (for logs and rate limits) from the **last** `X-Forwarded-For` entry. Leave off if clients can reach the gateway directly: they could forge it |
| `-web-dir` | `ODRAZ_WEB_DIR` | built-in reference UI | serve this folder as the web UI (for example your UI's `dist/`). Read from disk on each request, so edits show on refresh |
| `-healthcheck` | | | probe `-listen` at `/healthz` and exit 0 if alive (used by the image's HEALTHCHECK) |

## Sessions

| Flag | Variable | Default | |
|---|---|---|---|
| `-session-file` | `ODRAZ_SESSION_FILE` | none: memory only | keep sessions, tokens and users' editor settings in this file, encrypted. The image uses `/data/sessions.bin` (mount a volume at `/data`) |
| | `ODRAZ_SESSION_KEY` | | **required with `-session-file`**: base64 of 32 random bytes. `openssl rand -base64 32`. Keep it out of the image and the repository. Losing it loses the sessions (users sign in again); changing it makes the gateway refuse to start with the old file |

Without `-session-file` sessions live in memory: every restart signs everyone out. Sessions expire 12 hours after they
were created.

## OIDC sign-in

An OpenCloud backend can sign users in through the identity provider it trusts (the gateway discovers it through
the server, then runs the authorisation-code flow with PKCE). You must register the gateway as a client there:

| Flag | Variable | Default | |
|---|---|---|---|
| `-client-id` | `ODRAZ_CLIENT_ID` | `odraz-gateway` | the client id registered in the identity provider |

Register the **redirect URI** `PUBLIC_URL/auth/callback` (for example `https://files.example.com/auth/callback`), and
make it a public client (PKCE, no client secret). Nextcloud signs in with app passwords only (browser sign-in needs its
`user_oidc` app and is not supported yet).

## Read-only mode

`-read-only` (`ODRAZ_READ_ONLY=1`) turns the gateway into a viewer: browsing, previews, downloads and zips work, the
editor opens documents in view mode, and everything that changes something is refused with `403 permission`
(upload, new folder, rename, move, copy, delete, Photos folder choice, a user's own editor settings). Every drive is
reported `readOnly`, so a UI greys the actions out. It is for showcases and kiosks; it adds to, and never relaxes,
what the backends themselves allow. Signing in and out still work.

## Limits

| Flag | Default | |
|---|---|---|
| `-max-upload-bytes` | 5 GiB | largest single upload; bigger is answered `413 too_large` |
| `-auth-rate` | 20 | sign-in attempts per minute per client address (`-1` turns it off) |
| `-api-rate` | 6000 | other API calls per minute per client address (`-1` off). High on purpose: a photo gallery loads hundreds of thumbnails |

Rate limits are per gateway process and per address: a guard against floods and password guessing, not a replacement
for your reverse proxy's limits. A zip is limited to 500 items. The server has a 10 s header timeout and a 2 min idle
timeout; there is no overall read or write timeout because uploads and downloads can be large.

## Document editor

See [editor.md](editor.md) for what these do.

| Flag | Variable | Default | |
|---|---|---|---|
| `-office-url` | `ODRAZ_OFFICE_URL` | none | the ONLYOFFICE Document Server for **everyone**; users cannot change it. Empty: each user may set their own in Settings |
| `-office-secret` | `ODRAZ_OFFICE_SECRET` | | its JWT secret (required with `-office-url`) |
| `-office-allow-private` | `ODRAZ_OFFICE_ALLOW_PRIVATE` | off | let **users'** editor URLs point at private and loopback addresses (needed when the editor runs in the same Docker network). The operator's own URL is never restricted |
| `-office-gateway-url` | `ODRAZ_OFFICE_GATEWAY_URL` | the public URL | where the **editor** reaches this gateway, when that differs |
| `-office-listen` | `ODRAZ_OFFICE_LISTEN` | none | serve only the editor's two callback routes on this extra address |

## Logging

JSON lines on standard error (`log/slog`). One `request` line per request with the request id (also sent as the
`X-Request-Id` response header), method, path **without the query string**, status, bytes, milliseconds and client
address. `/healthz` is not logged. Cookies, headers, bodies and credentials are never logged.

## As a library

`gateway.Config` mirrors all of this (`Backends`, `PublicURL`, `ClientID`, `Listen`, `TrustProxy`, `Limits`, `Store`,
`ReadOnly`, `Middleware`, `Office`, `WebDir`, `WebFS`, `Logger`) plus `DemoLogin`, which only a program that embeds the gateway can set.
