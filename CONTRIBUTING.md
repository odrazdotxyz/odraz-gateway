# Contributing

Thanks for helping. Small, focused changes with tests are easiest to review.

## Set up

Go 1.23+. No other tools are needed to build and test.

```sh
git clone https://github.com/odrazdotxyz/odraz-gateway && cd odraz-gateway
go build ./... && go vet ./... && go test ./...
go run ./cmd/odraz-gateway -backend 'vendor=nextcloud,url=https://nc.example.com' -web-dir examples/vanilla   # edit the UI, refresh
```

## Before you open a pull request

* `gofmt -l .` prints nothing; `go vet ./...`, `go test -race ./...` pass; `staticcheck ./...` and `govulncheck ./...`
  are clean (CI runs all of them).
* **Behaviour changes come with tests.** The gateway is tested against an in-memory backend and a fake editor server,
  so no real storage server is needed (`pkg/gateway/editor_test.go` has the rig).
* **The API is a contract.** Changing a route means changing `api/openapi.yaml` and `docs/api.md` in the same commit; a
  test fails if the routes and the spec disagree. Keep `/api/v1` compatible: add, do not rename or remove.
* **Docs in the same commit** as the behaviour. A new flag goes into `docs/configuration.md`; a security-relevant
  decision into `docs/security.md`.
* One topic per commit, a message that says *why*. Keep unrelated clean-ups out.

## Rules this project holds to

* The gateway talks to storage **only through the Odraz Go SDK**. A vendor quirk belongs in the SDK adapter, not here.
* No vendor administration (users, groups, quotas) in the API or the UI.
* The browser never sees a storage credential or the editor secret. A new response must not contain one.
* The reference UI stays dependency-free (no build step, no npm).
* No secrets, tokens, personal data or private URLs in code, tests, docs or history.

## Licence

By contributing you agree that your work is licensed under the [GNU AGPL-3.0](LICENSE) like the rest of the project.
