# Security

What the gateway protects, how, and what it does **not** do. If you find a problem, see [../SECURITY.md](../SECURITY.md).

## What is valuable

* The **tokens** the gateway holds for each user's storage servers (app tokens, app passwords, OIDC access and refresh
  tokens): they open the user's files.
* The **files** that stream through it.
* A user's **editor secret**, if they set their own.

The browser never sees any of them. It holds only an opaque session id.

## Sessions

* A session id is 256 random bits, in a cookie that is `HttpOnly` and `SameSite=Lax`, and `Secure` when
  `-public-url` is `https://`. Behind TLS that terminates at a proxy, **set `-public-url` to the https address**.
* Sessions end 12 hours after they began (not extended by use), when the user disconnects their last server or
  signs out (`POST /auth/logout`), or when the id is no longer in the store.
* **Fixation.** A browser that is not signed in gets a **new** session id at the moment of sign-in and the old one is
  destroyed, so an id planted in a browser beforehand never becomes a signed-in session. Signing in to a second server
  while already signed in keeps the session.
* **At rest.** With `-session-file` everything (tokens, OIDC state, editor settings) is in one file, sealed with
  AES-256-GCM under `ODRAZ_SESSION_KEY`, written atomically, mode 0600, and the gateway refuses to start if the key is wrong or
  the file was altered. Without it, everything is in memory. **The key is the crown jewel**: whoever has the file and
  the key has every user's tokens. Do not bake it into an image; do not commit it; rotate by deleting the file (users
  sign in again).
* Not done: idle timeouts, per-device session lists, revoking one user's sessions from outside (delete the file),
  several gateway instances sharing sessions.

## Cross-site requests (CSRF)

Every call that changes anything must carry `X-Requested-With: odraz`. A browser cannot add a custom header to a
cross-site form post or to a cross-origin request without a CORS preflight, and the gateway answers no CORS
preflights, so another origin cannot make a signed-in browser change anything. Cookies are `SameSite=Lax` as a second
layer. Read calls are not CSRF-protected but are not readable cross-origin either (no CORS headers).

**Your own UI must be served from the same origin as the gateway** (the gateway serves it with `-web-dir`, or a reverse
proxy puts both under one host). A UI on another origin would need CORS with credentials, which the gateway does not
enable on purpose. See [building-a-ui.md](building-a-ui.md).

## What the page may load (CSP) and other headers

The page (`/` and `/index.html`) is served with
`default-src 'self'; script-src 'self' [editor]; frame-src [editor|'none']; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'`.
The editor origin appears only when an editor is configured, and is exactly that origin. Every response carries
`X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin` and `X-Frame-Options: DENY`; API answers are
`no-store`. **HSTS is not set by the gateway: set it at your TLS-terminating proxy.**

Files from a storage server are untrusted content. Served with `inline=1` only a short list of types is shown in the
browser (images, text, JSON, CSS/JS, mp4/webm/ogg); HTML and SVG get a `sandbox` CSP so they cannot run script in the
gateway's origin; everything else downloads. Thumbnails and avatars are only passed on if they are `image/png`,
`jpeg`, `gif` or `webp`.

## Sign-in

* The gateway receives a user name and an **app token or app password**, never requires the main account password,
  and passes them to the storage server to check. They are stored only in the session store.
* Sign-in attempts are limited per client address (default 20 a minute). Behind a proxy, set `-trust-proxy` so the
  limit counts real clients and not the proxy: but only if clients cannot reach the gateway except through it.
* OIDC uses the authorisation-code flow with PKCE and a random `state`; the redirect URI is fixed from `-public-url`.
* Per-backend TLS: certificates are verified unless that backend has `insecure=true`. A self-signed server should be
  fixed with a proper certificate; `insecure` means a network attacker between the gateway and that server can read
  and change everything.

## The demo login hook

`gateway.Config.DemoLogin` lets the **program that embeds the gateway** sign visitors in with a credential it holds:
the Odraz demo uses it for "Try the demo". It is not reachable from `odraz-gateway`: without the hook the route
`POST /auth/demo` is **not registered** (a test checks this) and `/api/v1/me` says `"demo": false`. If you embed the
gateway, never set the hook on a gateway that reaches real users' data.

## Server-side request forgery (SSRF) and the editor

The gateway makes outgoing requests to: the storage servers you configured (trusted), and the **editor server**.
A user's own editor URL is user-supplied, so it is refused if it is or resolves to a loopback, private, link-local,
carrier-grade-NAT or other internal address, unless the operator sets `-office-allow-private`. The check runs on the
address actually connected to, so DNS rebinding does not get around it; redirects are never followed; no proxy
from the environment is used. The address the editor names for a saved document must be on the editor's own origin.
The editor's secret never reaches the browser, and the editor's two callback routes need an unguessable id **and** a
valid HS256 token. Details: [editor.md](editor.md#security).

## Limits and abuse

Upload size (default 5 GiB), zip size (500 items), and per-address rate limits ([configuration.md](configuration.md#limits)).
There are no per-user quotas, no virus scanning, and no audit log beyond the access log. Free-for-all public
deployments need more (see the public demo plan).

## Logs

JSON, with request ids. Paths are logged without their query strings (those name files); cookies, headers, bodies and
credentials are not logged; error messages from storage servers can contain paths. Treat logs as sensitive anyway.

## Threat model summary

| Threat | Defence |
|---|---|
| Stolen session cookie | HttpOnly, Secure, 12-hour life; tokens are not in it. Still: whoever has it is the user until it expires |
| XSS in the UI | strict CSP, no inline script; files served inline are sandboxed |
| Another site making a signed-in browser act | custom-header CSRF check, no CORS, SameSite=Lax |
| Session fixation | new id at sign-in |
| Password guessing | rate limit; app tokens, not passwords |
| Someone with the session file | AES-256-GCM, key in the environment |
| Malicious editor URL (SSRF) | internal addresses refused at connection time; no redirects |
| Forged editor callback | unguessable id + signed token + origin check |
| A malicious storage server | its answers are untrusted: sandboxed content, image-type allowlist, size limits on editor saves; it could still serve large or slow responses |
| Embedding program installs a demo login | documented; absent from the product binary |
