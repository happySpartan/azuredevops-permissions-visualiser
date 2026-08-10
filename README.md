# Azure DevOps Permissions Visualiser

A single-executable, online permissions visualiser for Azure DevOps: collect a
point-in-time view of YAML pipeline and pipeline-folder permissions from one
organization and explore or export it.

## Architecture

Monorepo with two parts, packaged as one Go binary:

- `backend/` — Go backend: loopback web server, typed JSON API, local SQLite
  storage, point-in-time collection. The built frontend is embedded via
  `go:embed`.
- `web/` — React + TypeScript SPA (Vite). Builds into `backend/web/dist`, which
  the Go binary embeds at compile time.

Both the native executable and the container image run the same binary, bound
to `127.0.0.1` by default (single-administrator, local/private).

## Prerequisites

- Go 1.23+
- Node.js 20+ and npm
- Azure CLI (required, not bundled) for authentication

## Build and run

```sh
make web       # build the React frontend
make backend   # build the Go binary -> bin/visualiser
make build     # both of the above
make run       # run the backend (serves the built frontend)
```

Visit `http://127.0.0.1:8080`. The `/api/health` endpoint reports status.

Development mode (live reload + API proxy):

```sh
# terminal 1
make dev        # Vite dev server on :5173
# terminal 2
make run        # Go backend on :8080
```

## Container

```sh
make docker     # multi-stage build -> azuredevops-permissions-visualiser:latest
```

The image binds to localhost and mounts a dedicated Azure CLI configuration
directory read-only (never copying credentials into the image).

## Design decisions

Product and technology decisions (interview transcript, resolved constraints,
open questions) are recorded in `docs/product-discovery.md`. Domain glossary:
`CONTEXT.md`. Architectural decisions live in `docs/adr/` as they are made.