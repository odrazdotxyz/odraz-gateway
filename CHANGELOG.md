# Changelog

All notable changes. The format follows [Keep a Changelog](https://keepachangelog.com/); versions follow
[Semantic Versioning](https://semver.org/) (`/api/v1` changes only compatibly).

## [Unreleased]

## [0.1.0] - 2026-11-24

First public version.

### Added

- Read-only mode (`-read-only`, `Config.ReadOnly`): every change is refused, drives are reported read-only and documents open in view mode.
- The gateway as a library (`pkg/gateway`) and a binary (`cmd/odraz-gateway`) configured by flags and environment.
- API under `/api/v1` with an OpenAPI contract (`api/openapi.yaml`) checked against the routes by a test.
- Several storage servers per session (OpenCloud, Nextcloud), each with its own TLS setting.
- Sign-in with app tokens or passwords and, for OpenCloud, OIDC with PKCE.
- Drives, listing, ranged download, upload, folders, rename/move/copy (also across servers), delete, thumbnails,
  avatars, quotas, streamed zip downloads, the Photos section with its `.odraz/manifest.json` convention.
- A document editor host for ONLYOFFICE Document Server: one editor for every server, saves back to where the file
  came from, keeps a newer file and saves the edit as a copy, SSRF-safe user-supplied editor URLs, operator-locked or
  per-user settings, an editor-only listener.
- A session store interface with an in-memory and an AES-256-GCM encrypted file implementation.
- Request ids and structured access logs, `/healthz`, per-address rate limits, upload size limit, graceful shutdown.
- A 9 MB non-root container image with a health check; CI (format, vet, race tests, govulncheck, staticcheck).
- A reference web UI (`examples/vanilla`), a new session id at sign-in (against session fixation).

### Security
- The demo-login route does not exist unless an embedding program installs a hook; the binary never does.
