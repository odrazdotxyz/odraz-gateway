# The document editor

Word, spreadsheet, presentation and text files from **any** connected server open in one editor, and are saved back to
the server they came from. The editor is a separate service the gateway talks to over HTTP; the gateway never bundles
it. First provider: ONLYOFFICE Document Server. The code is split (`internal/editor` knows the editor, the gateway
knows storage) so another provider (WOPI, for Collabora) can be added.

## How it works

```
 browser ──(1) POST /api/v1/editor/session──▶ gateway ──SDK──▶ OpenCloud / Nextcloud / ...
    │                                         ▲  │
    │◀── config (signed, no secret) ──────────┘  │ (3) GET /editor/file/<id>   file bytes via the SDK
    │                                            │ (4) POST /editor/callback/<id>   save: gateway fetches the result
    └──(2) loads api.js, opens an iframe──▶ Document Server ──┘ and writes it back via the SDK
```

1. **Open.** The page asks for a session for `{acct, drive, path}`. The gateway checks the user can see the file,
   chooses *edit* or *view* (PDF and old binary formats such as `.doc` open read-only, because saving would convert
   them; read-only drives open read-only), makes an unguessable session id, and returns the editor configuration with
   its JWT signature. The shared secret never leaves the gateway.
2. **Load.** The page loads `api.js` from the editor server and creates the editor in an iframe.
3. **Download.** The editor fetches `document.url` (a gateway URL containing the session id) with a JWT in the
   `Authorization` header. The gateway verifies it, then streams the file through the SDK.
4. **Save.** About ten seconds after the last person closes the document the editor POSTs a callback (status 2). The
   gateway verifies the token, requires the result URL to be on the editor server's own origin, downloads it and writes it
   with `files.Upload(..., Overwrite: true)`. It answers `{"error":0}`. Status 4 (closed, no changes) writes nothing;
   3 and 7 (editor errors) are recorded; 1 (people connecting) is ignored; 6 (force-save) writes like 2 and keeps the
   session. The page polls `GET /api/v1/editor/session?id=` after you close the editor and says "Saved", "No changes", or
   what went wrong.

`document.key` is a hash of backend, drive, path and the file's version (ETag), so a changed file gets a new key and the
editor never serves a stale cached copy. Keys use only `[0-9a-z]` and stay far under the 128-character limit.

### Files changed behind the editor's back

The gateway remembers the file's version when the editor opened it. If the version differs when the editor saves, the
newer file is **not touched**: the edited document is written next to it as `name (edited copy YYYY-MM-DD HHMM).ext`
and the page says so. Nothing is lost either way. Limit: the check and the write are two calls, not one conditional
request, so a change in the few milliseconds between them can still be overwritten. A real fix is a conditional write
(`If-Match`) in the SDK; not done yet (OpenCloud ignores `If-None-Match` on `PUT`, so each vendor needs checking).
Several people editing one file at once share one editor session (same key); the save is made through the session of
the first person who opened it.

## Who sets the editor server

| | Where | Who it applies to |
|---|---|---|
| Operator | environment or flags (below) | everyone; Settings shows it read-only |
| User | Settings → Document editor (address and secret, "Check connection") | that user only |
| Neither | | no editor, no Edit action, nothing else changes |

The environment wins. A user's own setting is write-only: the secret is never sent back to the page (the status call
says only `secretSet: true`). **It is kept in the gateway's memory with the session**, so it is lost on restart until the
persistent encrypted session store exists.

| Environment | Flag | Meaning |
|---|---|---|
| `ODRAZ_OFFICE_URL` | `-office-url` | the editor server as browsers and the gateway reach it |
| `ODRAZ_OFFICE_SECRET` | `-office-secret` | its JWT secret (prefer the environment: flags show in the process list) |
| `ODRAZ_OFFICE_ALLOW_PRIVATE` | `-office-allow-private` | let *users'* editor URLs point at private/loopback addresses |
| `ODRAZ_OFFICE_GATEWAY_URL` | `-office-gateway-url` | where the **editor** reaches the gateway, if not the public URL |
| `ODRAZ_OFFICE_LISTEN` | `-office-listen` | serve only the editor's two routes on this extra address |

## Security

* **Secret.** Only on the gateway. Used to sign the configuration, to verify the editor's download and callback calls,
  and for "Check connection". No response contains it (tested).
* **The editor's calls carry no cookie.** `/editor/file/<id>` and `/editor/callback/<id>` need the unguessable id (256
  random bits, expires after 12 hours, dies when the user signs out) **and** a valid HS256 JWT. Only `HS256` is
  accepted, never `none`. The signed content, not the unsigned body, is what the gateway believes.
* **SSRF.** A user-supplied URL is refused if it is, or resolves to, a loopback, private, link-local, CGNAT or other
  internal address, unless the operator sets `ODRAZ_OFFICE_ALLOW_PRIVATE`. The check also runs on the address actually
  connected to (after DNS resolution), so a name that changes its answer later is caught. Redirects are never followed
  and no proxy from the environment is used. The operator's own URL is trusted. The URL the editor gives for a saved
  document must be on the editor's origin or the save is refused.
* **Page policy.** The page's CSP allows scripts and frames from exactly the configured editor origin and nothing else.
  With no editor, `frame-src 'none'`. After a user saves their own address the page reloads so the policy applies.
* **Separate listener.** With `-office-listen 0.0.0.0:8081` the editor's two routes are served there and **nowhere
  else is reachable from that address**: not the API, not the page, not the demo login. Without it they share the main
  address (fine when the editor can reach the gateway's public URL).

## Networking

The editor must reach the gateway and the gateway must reach the editor. The demo gateway listens on
`127.0.0.1:8080`, which a container cannot reach, so `make run-office` adds the editor-only listener on `0.0.0.0:8081`
and tells the editor to use `http://host.docker.internal:8081` (compose maps it with `extra_hosts: host-gateway`).
The container also needs `ALLOW_PRIVATE_IP_ADDRESS=true`, because the Docker host is a private address to it. In
production put both behind one reverse proxy and domain (the editor under a path or sub-domain of the gateway's), and
keep that setting off.

## Running the demo editor

```
make demo && make nextcloud     # the two servers, as usual
make office                     # ONLYOFFICE Document Server on http://localhost:8083 (2.1 GB image, first start ~1 minute)
make run-office                 # the gateway with both servers and the editor
node scripts/editor-walkthrough.mjs light ./shots    # real browser, both servers (SKIPS when no editor is configured)
```

**Resources** (measured here with the demo load; ONLYOFFICE's own reference: 4 GB RAM, 2 GHz dual core, 40 GB disk,
4 GB swap, about 100 open documents on one 2.8 GHz core and 4 GB): the container used about 340 MiB idle after
editing four documents, and peaked at about 1.24 GiB (cgroup peak, including start-up). I did **not** measure with memory
caps of 512 MB, 1 GB and 2 GB, so whether it runs on a small VPS is unverified; the vendor figure says it will not fit
500 MB. The gateway only needs the editor's address and secret, so the editor can run on another machine. The community
edition reportedly limits simultaneous editing connections (community sources say 20); the official FAQ does not
confirm a number, so none is stated here.

## Licence note

ONLYOFFICE Docs Community Edition is AGPL-3.0, "for testing, personal use, development, and small-scale deployments",
with branding kept and no white-labelling without a commercial licence. We run it as a separate service and talk to it
over HTTP; we neither bundle nor modify it. Have a licence review confirm that reading before a **public** demo.

## API

All routes are under `/api/v1`; `/auth/*` (browser redirects) and `/editor/*` (called by the editor server) are not versioned.

| Route | |
|---|---|
| `GET /api/v1/editor` | `{configured, locked, url, secretSet, provider, persistent}` |
| `PUT /api/v1/editor` `{url, secret}` | save the user's own editor (403 when the operator set one); empty secret keeps the saved one |
| `DELETE /api/v1/editor` | forget it |
| `POST /api/v1/editor/check` | `{ok}` or `{ok:false, error}`: reachable, and does it accept our secret |
| `POST /api/v1/editor/session` `{acct, drive, path}` | `{id, script, mode, config}`; 409 no editor, 415 unsupported type |
| `GET /api/v1/editor/session?id=` | `{state: open\|saved\|unchanged\|conflict\|error\|gone, savedTo, message}` |
| `GET /editor/file/{id}`, `POST /editor/callback/{id}` | called by the editor only |

## Tests

`internal/editor` (JWT, keys, formats, SSRF rules, callback forms, check) and `pkg/gateway/editor_test.go` run
against a **fake Document Server** and an in-memory backend, so CI never needs the container: save, the
conflict copy, forged and unsigned calls, a foreign download URL, sign-out, the secret never appearing in a response,
per-user settings, private addresses refused, the CSP, and the editor-only listener. The browser walkthrough uses the real
container with one `.docx` on OpenCloud and one on Nextcloud.
