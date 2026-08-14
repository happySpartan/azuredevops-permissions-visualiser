# Progress tracker

Status legend: `[ ]` pending · `[/]` in progress · `[x]` completed (kept for history)

This is the canonical task tracker for the repository. Update it whenever a task
changes status. Specs and decisions live in `docs/product-discovery.md`; ADRs
live in `docs/adr/`.

## Released

- [x] v1.0.0 — YAML pipeline + pipeline-folder permissions explorer (projects → root folder → nested folder → pipeline), subject→resources and resources→subjects navigation, effective-permission explanations with Direct/Inherited and User/Via-group provenance, scoped permission matrix, group membership explorer, per-view CSV export, live collection progress, ACL identity resolution, single embedded Go + React binary (Linux/Windows x64), ghcr.io container image.
  Tag `v1.0.0` at `511fdc8`; release URL: https://github.com/happySpartan/azuredevops-permissions-visualiser/releases/tag/v1.0.0

## Backlog

### Collection & namespaces

- [x] Git namespace: repositories + branches (Git Repositories namespace `2e9eb7ed-3c0a-47d4-87c1-0ffdd275fd87`). Collect repos and head refs per project, ACLs for repo and branch tokens, namespace-aware store, resources explorer hierarchy, subject/resource/explanation views decode against the correct namespace's actions, run-wide CSV export includes namespace column.
- [x] Pipeline resource permissions: agent pools (BuildAdministration `302acaca-b667-436d-a946-87133492041c`, org-level, tokens `pools/<poolId>`), service connections (ServiceEndpoints `49b48001-ca20-4adc-8111-5b60c903a50c`, tokens `<projectId>/<endpointId>`), variable groups (Library `b7e84409-6553-448a-bbb2-af228e07cbeb`, tokens `<projectId>/<variableGroupId>`). Tables, tx writers, run counts, permissionResource inference, resources explorer sections, overview stats, export.
- [ ] Classic build pipelines (non-YAML build definitions share the Build namespace; add the classic-definition collection path)
- [ ] Org/project-level namespaces beyond Build and Git (e.g. WorkItemTracking, VersionControlItems) — decide scope before starting

### Platform & engineering

- [x] CI container job: resolve trivy HIGH/CRITICAL findings in the azure-cli base image. Go 1.25.12→1.26.6 clears stdlib findings (CVE-2026-39821, CVE-2026-46600; 1.25.13 alone did not cover the latter); runtime image pinned to `azure-cli:2.89.1` + `tdnf update` clears OS-package findings (libarchive/libssh2/python3); remaining bundled-pip-wheel findings (cryptography/msgpack/setuptools, frozen in azure-cli's venv, no fixed wheel shipped upstream) are documented in `.trivyignore.yaml` as accepted-risk/tracked-upstream.
- [ ] Cosmetic: commit `511fdc8` message has a shell-eaten backtick phrase. Not worth rewriting pushed history; leave as-is unless history is ever rewritten.

## Out of scope (decided — not planned)

- Classic release pipelines (distinct permissions path) — explicitly out of scope.
- Native `.xlsx` workbook export — explicitly out of scope; CSV remains the only export format.

## Notes

- Issue tracker: GitHub Issues is intentionally **not used** (empty). See `docs/agents/issue-tracker.md`.
- Workflow: complete work → `make build` + `go vet ./...` + `go test ./...` + frontend `npm run build`/`npm test` green → commit AND push to `origin/main`.
- Live verification against the real org requires `AZDO_ORG` (e.g. `happyspartan`) and an authenticated Azure CLI (`az account get-access-token --resource 499b84ac-1321-427f-aa17-267ca6975798`); MSA pass-through header needed for MSA accounts.
