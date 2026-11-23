# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for a security problem. Use GitHub's private reporting:
*Security → Report a vulnerability* on this repository. Include what you found, how to reproduce it, and what you
think the impact is. You will get an answer within a few days; fixes for confirmed problems are released as soon as
they are ready, and you are credited if you wish.

## Supported versions

Only the latest release receives fixes while the project is 0.x.

## Scope

In scope: the gateway (`cmd/odraz-gateway`, `pkg/gateway`), its API, its container image, and the reference UI.
Things we would like to hear about in particular: ways to read or change another user's session or files, to reach
the editor routes without a valid token, to make the gateway connect to an internal address when it should not, to leak
a token or secret into a response or a log, or to run script in the gateway's origin.

Out of scope: weaknesses in the storage servers or the document editor themselves (report those to their projects),
and anything that needs an operator to have turned on a documented unsafe option (`insecure=true`,
`-office-allow-private`, an embedding program's demo hook). The threat model is in [docs/security.md](docs/security.md).
