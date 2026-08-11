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

- Go 1.25+
- Node.js 20+ and npm
- Azure CLI (required, not bundled) for authentication

## Build and run

```sh
make web       # build the React frontend
make backend   # build the Go binary -> bin/visualiser
make build     # both of the above
make release   # Linux x64 and Windows x64 binaries -> dist/
make run       # run the backend (serves the built frontend)
```

Visit `http://127.0.0.1:8080`. The `/api/health` endpoint reports status.
The native executable opens this URL in the default browser. Set `NO_BROWSER=1`
to disable automatic browser launch (recommended for containers and services).

JSON API (v1):

- `GET /api/health` — service status.
- `GET /api/run/current` — the latest completed analysis run (or `null`).
- `POST /api/run/collect` — run a one-shot collection of the configured
  organization (`AZDO_ORG`); requires Azure CLI authentication. A failed or
  cancelled run is discarded and never becomes an analysis; the previous good
  run is preserved. Returns `202 Accepted`; collection continues in the
  background and concurrent attempts return `409 Conflict`.
- `GET /api/run/collection-status` — report the active collection phase,
  lifecycle state, completion counts, or actionable failure details.
- `POST /api/run/delete` — delete all collected data.
- `GET /api/explorer/subjects` — search and page through collected users and
  groups (`search`, `kind`, `limit`, and `offset` query parameters).
- `GET /api/explorer/subjects/permissions` — show Azure DevOps' reported
  effective Build permission results for one subject (`descriptor`).
- `GET /api/explorer/groups/memberships` — show a selected group's direct and
  transitive members, including nested groups and deterministic membership paths
  (`descriptor`). Cycles in collected membership data are handled safely.
- `GET /api/explorer/groups/memberships/export` — download the selected group's
  direct and transitive members and membership paths as CSV (`descriptor`).
- `GET /api/explorer/subjects/explanation` — explain one subject/resource/action
  tuple with raw ACEs, nested group paths, and resource ancestry (`descriptor`,
  `token`, and `bit`).
- `GET /api/explorer/subjects/export` — download the current subject's effective
  permission rows as CSV (`descriptor`).
- `GET /api/explorer/resources` — list the collected project, pipeline-folder,
  and YAML pipeline hierarchy.
- `GET /api/explorer/resources/permissions` — show which subjects have
  effective permissions on one resource (`token`).
- `GET /api/explorer/resources/export` — download the active resource view as
  CSV, scoped to one resource (`token`).
- `GET /api/explorer/matrix` — compare subjects and secured resources for one
  permission action within one project (`projectId` and `bit`).
- `GET /api/explorer/matrix/export` — download the active matrix view as CSV,
  scoped to one project and permission action (`projectId` and `bit`).
- `GET /api/run/export/effective-permissions` — download a flat CSV of every
  subject × resource × action with effective state and provenance flags.
- `GET /api/run/export/assignments` — download a raw ACE CSV with bitmask
  columns in hex.

The embedded frontend provides the run overview plus subject and resource entry
points into the access explorer. Selecting a group keeps it as a first-class
permission subject and also shows its direct and transitive membership, nested
groups, membership paths, and a group-membership CSV export.

Per-view CSV exports repeat the active resource identity or matrix filters on
every row. Matrix exports include both collected permission results and
`unknown` cells where no assignment was collected. Values that could be
interpreted as spreadsheet formulas are apostrophe-prefixed for safety.

Collected data is stored in a per-user data directory (SQLite):
`AZDO_VIS_DATA_DIR` overrides the default (`%LOCALAPPDATA%\AzureDevOpsPermsVisualiser`
on Windows, `~/.local/share/azuredevops-permissions-visualiser` on Linux). Only
the latest completed run is retained.

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
docker run --rm -p 127.0.0.1:8080:8080 \
  -e AZDO_ORG=https://dev.azure.com/your-org \
  -v "$HOME/.azure:/root/.azure:ro" \
  azuredevops-permissions-visualiser:latest
```

The image listens inside its private container network. Publishing it explicitly
to `127.0.0.1` keeps the application host-local. Mount a dedicated Azure CLI
configuration directory read-only; credentials are never copied into the image.
The runtime image includes Azure CLI because collection obtains its access token
through `az account get-access-token`; the host's credential directory supplies
the authenticated account state.

## Design decisions

Product and technology decisions (interview transcript, resolved constraints,
open questions) are recorded in `docs/product-discovery.md`. Domain glossary:
`CONTEXT.md`. Architectural decisions live in `docs/adr/` as they are made.