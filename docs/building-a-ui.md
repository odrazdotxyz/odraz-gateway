# Building your own UI

The gateway serves any static web app. Point it at a folder (`-web-dir ./dist`), or run your UI behind the same
reverse proxy as the gateway, **on the same origin**. Everything it does goes through [the API](api.md). The reference
UI in [`examples/vanilla`](../examples/vanilla) (about 1,500 lines of plain JavaScript, no build) uses every route and
is the easiest thing to read alongside this page.

## Swapping the UI

```sh
odraz-gateway -backend '...' -web-dir ./my-ui/dist      # served from disk; edit, refresh
```

Anything under the folder is served as a static file (`GET /`; `index.html` at the root). Paths beginning with
`/api/`, `/auth/`, `/editor/` and `/healthz` belong to the gateway. A single-page app with client-side routes should use
hash routing (`#/path`), because the gateway does not rewrite unknown paths to `index.html`.

## 1. Who is signed in

`GET /api/v1/me` works without a session:

```json
{"authenticated": false, "demo": false,
 "backends": [{"id":"work","vendor":"opencloud","connected":false,"oidc":true,"demo":false},
              {"id":"home","vendor":"nextcloud","connected":false,"oidc":false,"demo":false}]}
```

Show a sign-in for each backend that is not connected.

## 2. Signing in

* **App token / app password** (any backend): `POST /auth/apptoken` with JSON `{"backend","username","token"}` and the
  CSRF header. `200 {"ok":true}` and a `Set-Cookie`; `401` when the server refuses; `429` when throttled.
* **Browser sign-in** (when `oidc` is true): navigate the browser to `/auth/login?backend=<id>`. The gateway redirects
  to the identity provider and, after login, back to `/`. Handle the return by calling `/api/v1/me` again.
* **Several servers:** sign in to a second one the same way; the session keeps both. `POST /auth/disconnect?backend=<id>`
  forgets one; disconnecting the last ends the session. `POST /auth/logout` ends it at once.

## 3. The session cookie and CSRF

The cookie `odraz_session` is HttpOnly: you cannot read it and need not. Use `fetch(url, {credentials: 'same-origin'})`.
**Every request that changes something needs the header `X-Requested-With: odraz`**, or it gets `403`:

```js
async function api(path, { method = 'GET', body, headers = {} } = {}) {
  const res = await fetch(path, { method, body, credentials: 'same-origin',
    headers: { 'X-Requested-With': 'odraz', ...headers } });
  if (!res.ok) { const j = await res.json().catch(() => ({})); throw Object.assign(new Error(j.error || res.statusText), { status: res.status, code: j.code }); }
  return res;
}
```

On `401` from any call, the session ended: go back to the sign-in.

## 4. Errors

Always `{"error": "...", "code": "..."}`; react to `code`, show `error` to people. The codes are in [api.md](api.md#conventions).

## 5. The calls you need first

```js
const drives = (await (await api('/api/v1/drives')).json()).drives;     // [{acct, id, name, kind, readOnly, used, total}]
const d = drives.find(x => x.kind === 'personal');
const list = await (await api(`/api/v1/list?acct=${d.acct}&drive=${encodeURIComponent(d.id)}&path=/`)).json();
// list.entries: [{name, path, isDir, size, modTime, mime}]
```

Always URL-encode `drive` and `path` (use `URLSearchParams`): OpenCloud drive ids contain `$`.

## 6. Uploads, downloads, ranges

* **Download:** link to `/api/v1/file?acct&drive&path` (the browser sends the cookie). Add `&inline=1` to show a
  picture or a video in the page. `Range: bytes=a-b` is honoured (206), which is how `<video>` seeks and how a text
  preview reads only its first 128 KB.
* **Upload:** `PUT /api/v1/file?acct&drive&path&overwrite=0|1` with the raw bytes as the body (not multipart). Use
  `XMLHttpRequest` for progress. `409 exists` means ask before replacing; `413` means over the limit.
* **Thumbnails:** `<img src="/api/v1/thumb?acct&drive&path&size=256">`; on `501` fall back to an icon (and for videos,
  to the first frame of the file).
* **Zip of a selection:** one `GET /api/v1/zip?item=acct|drive|path&item=...` navigation; it streams.
* **Between servers:** `POST /api/v1/copy` or `/move` with `toAcct` and `toDrive`.

## 7. Photos, editor, settings

Photos: `GET /api/v1/photos` for the combined list, `/photos/config` for which folders are chosen. Editor:
ask `GET /api/v1/editor`; if `configured`, offer "Edit" and `POST /api/v1/editor/session`, then load the `script` it
returns and create `new DocsAPI.DocEditor(containerId, config)` ([editor.md](editor.md)). The page's CSP must allow the
editor's origin: when you serve your own `index.html` the gateway sets the policy for you (it names the editor origin
automatically).

## 8. Design notes for UI authors

* The CSP forbids inline scripts: put your code in files. `style-src 'unsafe-inline'` is allowed.
* Keep the gateway's vocabulary: servers are `acct`, drives are `drive`, the three groups people understand are
  *Personal* (`kind=personal`), *Teams* (`project`), *Shared* (`shared`, `other`).
* A generated TypeScript client from `api/openapi.yaml` (for example with `openapi-typescript`) gives you types for all
  of the above; the Odraz UI repository does exactly that.
