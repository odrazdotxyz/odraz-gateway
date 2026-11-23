# Third-party notices

## Code the gateway is built from

| Component | Licence | Notes |
|---|---|---|
| Go standard library | BSD-3-Clause | |
| [odraz-go-sdk](https://github.com/odrazdotxyz/odraz-go-sdk) | Apache-2.0 | the only runtime dependency |
| gopkg.in/yaml.v3 | MIT and Apache-2.0 | tests only (checks the OpenAPI file) |

Regenerate the list with `go run github.com/google/go-licenses@latest report ./cmd/odraz-gateway`.

## Fonts and brand assets in `examples/vanilla/assets`

| File | Licence |
|---|---|
| `Poppins-Regular.ttf`, `Poppins-SemiBold.ttf` | SIL Open Font License 1.1 (`OFL.txt`) |
| `odraz-*`, `odrive-*`, `ogram-*` logos, `favicon.*` | Odraz brand kit, **CC BY-ND 4.0** (Attribution-NoDerivatives): use them unmodified and credit "Odraz". Do not alter, recolour or crop them. A fork that wants its own look should replace them with its own artwork |
| `odraz.css` | design tokens for the reference UI |

## Software the gateway talks to (not included)

The gateway does not bundle or modify any of these; it talks to them over HTTP.

* **OpenCloud** (Apache-2.0) and **Nextcloud** (AGPL-3.0): the storage servers.
* **ONLYOFFICE Docs Community Edition** (AGPL-3.0): the optional document editor, run by you as a separate service.
  Its licence terms apply to *you* when you run it (for example: modified versions offered over a network must publish
  their source; the Community Edition is intended for testing, personal use, development and small-scale deployments;
  white-labelling needs a commercial licence). Read them before offering an editor to others.
