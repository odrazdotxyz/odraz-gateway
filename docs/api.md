# The HTTP API

The contract is [`api/openapi.yaml`](../api/openapi.yaml) (OpenAPI 3.0); a test fails if it and the routes drift
apart. This page is the tour.

## Conventions

* **Versioned.** Everything a UI calls is under `/api/v1` and changes only compatibly. `/auth/*` (browser redirects
  and the sign-in form post) and `/editor/*` (called by the editor server) are outside the version.
* **Names.** Every file call names the connected server and the drive: `?acct=<backend id>&drive=<drive id>` and, for
  most, `&path=/absolute/path`. Get them from `/api/v1/me` and `/api/v1/drives`.
* **Session.** Cookie `odraz_session` (HttpOnly, SameSite=Lax, `Secure` on https), set at sign-in.
* **CSRF.** Every request that changes anything needs the header `X-Requested-With: odraz`. A browser cannot add it to a
  cross-site form post. Reads do not need it.
* **Errors.** JSON `{"error": "text for people", "code": "machine_readable"}` with a fitting status:

| code | status | |
|---|---|---|
| `auth` | 401 | not signed in, or the server refused the credentials |
| `permission` | 403 | |
| `not_found` | 404 | |
| `exists`, `not_empty` | 409 | target exists / folder not empty |
| `invalid_path` | 400 | bad path or parameters |
| `too_large` | 413 | over the upload limit |
| `rate_limited` | 429 | with `Retry-After: 60` |
| `unsupported` | 501 | the server cannot do it (for example video thumbnails on OpenCloud) |
| `blocked_by_proxy` | 502 | a proxy or login gateway answered instead of the server |

## A tour

```sh
# 1. sign in to a server (app password / app token); the cookie goes in the jar
curl -c jar -X POST localhost:8080/auth/apptoken -H 'X-Requested-With: odraz' -H 'Content-Type: application/json' \
  -d '{"backend":"nextcloud","username":"alice","token":"abcde-fghij-..."}'

# 2. who and what is connected
curl -b jar localhost:8080/api/v1/me
# 3. the drives (with quota), merged across every connected server
curl -b jar localhost:8080/api/v1/drives
# 4. list a folder
curl -b jar 'localhost:8080/api/v1/list?acct=nextcloud&drive=personal&path=/'
# 5. upload (raw bytes), download (a range), delete
curl -b jar -X PUT -H 'X-Requested-With: odraz' --data-binary @notes.txt \
  'localhost:8080/api/v1/file?acct=nextcloud&drive=personal&path=/Documents/notes.txt&overwrite=1'
curl -b jar -H 'Range: bytes=0-99' 'localhost:8080/api/v1/file?acct=nextcloud&drive=personal&path=/Documents/notes.txt'
curl -b jar -X DELETE -H 'X-Requested-With: odraz' 'localhost:8080/api/v1/entry?acct=nextcloud&drive=personal&path=/Documents/notes.txt'
```

## Routes

| | |
|---|---|
| `GET /api/v1/me` | backends and which are connected (works without a session) |
| `GET /api/v1/drives` | drives of all connected servers with `kind` (`personal`, `project`, `shared`, `other`), `readOnly`, `used`/`total` |
| `GET /api/v1/list` | direct children of a folder |
| `GET /api/v1/file` | download; single `Range`; `inline=1` shows images, text, JSON, CSS/JS, HTML and mp4/webm/ogg video in the browser instead of downloading (HTML and SVG are sandboxed so they cannot run in the gateway's origin); every other type is always a download |
| `PUT /api/v1/file` | upload, body = the bytes; creates parents; `overwrite=1` to replace |
| `GET /api/v1/thumb` | server-rendered thumbnail, `size` 16 to 1024 |
| `GET /api/v1/zip` | `item=acct\|drive\|path` repeated (max 500): streamed zip, items may come from different servers |
| `GET /api/v1/avatar` | the user's picture on one server |
| `POST /api/v1/mkdir`, `move`, `copy` | `move`/`copy` take `from`, `to`, and `toAcct`/`toDrive` to cross drives or servers (streamed through the gateway: slower, not atomic) |
| `DELETE /api/v1/entry` | `recursive=1` for folders |
| `GET /api/v1/photos`, `/photos/config`, `PUT/DELETE /photos/folder` | the Photos section ([manifest.md](manifest.md)) |
| `GET/PUT/DELETE /api/v1/editor`, `POST /editor/check`, `POST/GET /editor/session` | the document editor ([editor.md](editor.md)) |
| `GET /healthz` | liveness |
| `POST /auth/apptoken`, `/auth/disconnect`, `/auth/logout`, `GET /auth/login`, `/auth/callback` | signing in and out ([building-a-ui.md](building-a-ui.md)) |
| `POST /auth/demo` | only if the embedding program installed a demo login hook; absent from `odraz-gateway` |

## Vendor differences you will meet

The gateway hides most of them. A few leak through as `unsupported` or empty results: OpenCloud has no video
thumbnails (answer 501) and a user without a profile picture gets 404 from `/avatar`; Nextcloud's avatars always
exist (initials), quota changes can take minutes to show, and its Team folders appear as `project` drives.
