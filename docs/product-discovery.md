# Product discovery: Azure DevOps permissions visualiser

Status: active — Round 2 in progress

This document preserves the `grill-with-docs` interview, the user's answers, the decisions inferred from them, and the questions that remain open. Items under **Open questions** are recommendations only and have not been accepted.

## Product intent

Azure DevOps administrators currently struggle to determine which permissions users and groups have throughout an organization, including permissions on pipelines, pipeline folders, repositories, branches, and other secured resources.

The product will collect a point-in-time view of permissions from one Azure DevOps Services organization and let a technical administrator navigate and export the result.

## User's answer, preserved verbatim

> One organisation right now but support on multiple projects. Deployed locally given with an executable to start on the PC. Permissions like this are not saved on your PC. Possibility to "self-host" it via some image given to people and it can run as an web app. It is for technical administrator not just troubleshoot. The bundled executable proposes export in excel file like csv for technical admins. For now no snaopshots it is ran as "one-shot" and then we have the state. It can be stored somehow in a SQLLite or some cache or whatever file but all locally for now or via local from the volume of the app when dockerized. Today it is super hard for admins to know which permissions have users or groups in the whole organisation - inside the pipelines, each repos, each branches, etc.... We will target Azure DevOps Services for now only. Not a shared service for now. So after login we capture a point in time which takes some time and then the user navigates to the point in time analysis. The first version should allow the user to start the app, login to the organisation via Azure CLI then to see for each pipeline or groups of pipeline folders each permissions for user and groups.

## Questions asked and current answers

### Q1 — Primary outcome

**Question:** What must an Azure DevOps administrator be able to accomplish that is difficult today?

**Current answer:** Determine which permissions users and groups have throughout an Azure DevOps organization. The initial usable slice focuses on pipeline and pipeline-folder permissions across multiple projects.

### Q2 — First user and operating context

**Question:** Is this a personal troubleshooting tool, an internal shared service, or a production governance product?

**Current answer:** It is a tool for technical Azure DevOps administrators, not merely a one-off troubleshooting utility. The first version is not a shared service.

### Q3 — Azure DevOps target

**Question:** Azure DevOps Services, Azure DevOps Server, or both? One or multiple organizations?

**Current answer:** Azure DevOps Services only. One organization per analysis, containing multiple projects.

### Q4 — Authoritative resource scope

**Question:** Which secured-resource types belong in the first release?

**Current answer:** The broader problem includes pipelines, repositories, branches, and other organization resources. The first release must cover individual pipelines and pipeline-folder hierarchies. Other resource types remain future candidates and have not yet been prioritized.

### Q5 — Meaning of effective permission

**Question:** Should the product independently calculate effective permissions or rely on results exposed by Azure DevOps and explain the evidence?

**Current answer:** Decided. Azure DevOps is the source of truth. The product does not independently compute a competing verdict; it collects and presents Azure DevOps' own reported effective result and its evidence, then explains the contributing assignments, memberships, and inheritance.

### Q6 — Data acquisition and credentials

**Question:** How should the first version authenticate and obtain data?

**Current answer:** The administrator logs in through Azure CLI and selects/connects to an Azure DevOps Services organization.

### Q7 — Live versus snapshot behavior

**Question:** Should exploration be live, snapshot-based, or cached? Is historical comparison required?

**Current answer:** Each analysis is a one-shot, point-in-time collection. Collection may take time; after it finishes, the administrator navigates the collected state. Historical snapshot comparison is not required in the first version.

### Q8 — Delivery form

**Question:** Browser, desktop, IDE extension, CLI report, or static export?

**Current answer:** Provide a locally run executable for a PC. It should also be distributable as a container image and run as a web application.

### Q9 — Security and persistence boundary

**Question:** May collected permission data be stored locally, and what retention/security controls are required?

**Current answer:** No remote or centralized persistence in the first version. Temporary/local persistence may use SQLite, a cache, another local file, or a mounted local volume when containerized. The exact lifetime and deletion policy remain unresolved.

### Q10 — First usable release

**Question:** What concrete scenario must work end to end?

**Current answer:** An administrator starts the application, authenticates to one organization using Azure CLI, runs a point-in-time collection across multiple projects, and sees permissions assigned to users and groups for each pipeline and pipeline-folder grouping. The administrator can export results for further analysis.

## Resolved design constraints

- Audience: technical Azure DevOps administrators.
- Service target: Azure DevOps Services only.
- Organization scope: one organization per analysis.
- Project scope: multiple projects within that organization.
- Initial secured resources: YAML pipelines and pipeline folders (four levels: project, root folder, nested folder, pipeline).
- Data acquisition: one-shot, point-in-time analysis run; success-required (no partial analysis).
- Authentication entry point: Azure CLI, a required prerequisite on Windows and Linux x64 (not bundled).
- Deployment: local executable plus a container image capable of serving a web application; single Go binary reused in both.
- Persistence boundary: collected data remains local to the executable or container volume (SQLite in a per-user data dir); no centralized storage in v1.
- Export: per-view CSV export; native `.xlsx` is out of scope for v1.
- Collaboration: shared/multi-user service behavior is out of scope for v1.
- Identity types: users, Azure DevOps groups, and Microsoft Entra groups are first-class; permissions source of truth is Azure DevOps' own result.

## Open questions for the next interview round

These questions were asked but not answered. Recommendations are recorded to restart the discussion efficiently.

### Q11 — Local retention

What happens to the collected state after the application closes?

Options considered:

- Delete it automatically on close.
- Retain only the latest completed run.
- Retain multiple named runs.
- Ask after every run.

**Recommendation:** Retain only the latest completed run locally, replace it after the next successful run, and provide a prominent **Delete collected data** action.

**Decision:** Retain only the latest completed analysis run locally, replace it after the next successful run, and provide a prominent **Delete collected data** action.

### Q12 — Interrupted or partial collection

May an administrator analyze partial results when APIs fail, access is denied, throttling occurs, or collection is cancelled?

**Recommendation:** Yes. Save progress incrementally; classify the run as complete, partial, cancelled, or failed; and show exact coverage gaps.

**Decision:** No half data. Exploration is permitted only after a fully successful collection. Partial, cancelled, or failed runs are discarded and the previous successful run is preserved. The failure and its cause are still reported to the administrator.

### Q13 — Pipeline types

Does v1 include YAML pipelines, classic build pipelines, and classic release pipelines?

**Recommendation:** Include YAML and classic build pipelines. Defer classic release pipelines unless they are essential because they require a distinct permissions path.

**Decision:** V1 covers YAML pipelines only. Classic build and classic release pipelines are out of scope.

### Q14 — Pipeline hierarchy levels

Should v1 show permissions at project, root pipeline folder, nested pipeline folder, and individual pipeline levels?

**Recommendation:** Include all four so inherited permissions can be understood.

**Decision:** Include all four levels: project, root pipeline folder, nested pipeline folder, and individual pipeline, so inherited permissions can be understood.

### Q15 — Permission semantics and explanation

Should each subject/resource pair show explicit assignments, effective permissions, or both with an explanation of membership, inheritance, and allow/deny precedence?

**Recommendation:** Show the effective outcome by default and expose assignments plus explanation on demand.

**Decision:** Show both explicit assignments and effective permissions. Distinctly tag the provenance of each result — `Direct` versus `Inherited`, and `User` versus `Via group` — so the origin of a permission is always visible.

### Q16 — Navigation directions and identity types

Should users be able to navigate both subject-to-resources and resource-to-subjects? Should disabled users, service identities, build service accounts, and nested groups be included?

**Recommendation:** Support both directions. Include all identity types and provide filters rather than omitting identities.

**Decision:** Support both subject-to-resources and resource-to-subjects as two separate, distinct approaches (not simultaneously).

**Decision (identity types):** V1 collects and displays only three identity types: users, Azure DevOps-defined groups, and Microsoft Entra-backed groups. Service identities and other types are collected for completeness where they appear in ACLs but are not first-class in v1 navigation.

### Q17 — Group expansion

Should group permissions automatically expand to every transitive user?

**Recommendation:** Keep groups first-class in the default view. Expand membership on demand, with an optional flattened-user export.

**Decision:** Group-first. Groups remain first-class subjects in both the subject and resource views. Membership is expanded on demand, with an optional flattened-user export.

### Q18 — Permission operations

Should the UI expose every operation in the relevant Azure DevOps security namespace or only a curated subset?

**Recommendation:** Collect all operations. Prioritize common and high-risk operations in the UI and place the rest behind **Show all**.

**Status:** Decided. Collect all 15 permission actions in the Build security namespace (ID `d34d3680-dfe5-4cc6-a949-7d9c68f73cba`): `ViewBuilds`, `EditBuildQuality`, `RetainIndefinitely`, `DeleteBuilds`, `ManageBuildQualities`, `DestroyBuilds`, `UpdateBuildInformation`, `QueueBuilds`, `ManageBuildQueue`, `StopBuilds`, `ViewBuildDefinition`, `EditBuildDefinition`, `DeleteBuildDefinition`, `OverrideBuildCheckInValidation`, `AdministerBuildPermissions`. Pipeline folders are represented in this same Build namespace via the token hierarchy (`PROJECT_ID/<folder>/…`, `PROJECT_ID/<definitionId>`), covering project, root folder, nested folder, and pipeline levels. The UI surfaces the operationally meaningful actions prominently and places the specialized remainder behind **Show all**. Pipeline resource permissions (agent pools, service connections, variable groups — the BuildAdministration namespace) are deferred out of v1 scope.

### Q18b — Technology: single Go executable + React stack (DECIDED)

The user proposed that one self-contained Go executable should satisfy both the local-executable and containerized distributions, and confirmed the following stack:

**Decision:** Adopt a single Go executable as the backend: it embeds the static frontend via `go:embed`, serves a loopback web interface, calls the Azure DevOps REST API, and persists collected state locally via SQLite (`modernc.org/sqlite`, pure Go). The container distribution runs the same binary bound to localhost. Wide-angle trade-offs (concurrent collection with goroutines, trivial loopback HTTP server, embedded SQLite, easy cross-compilation for Windows/Linux x64, stdlib CSV export) were judged to outweigh the cost of maintaining a separate TypeScript frontend.

### Q25 — Frontend technology (DECIDED)

**Decision:** TypeScript + React, built separately and embedded into the Go binary via `go:embed` at compile time. This gives the rich tables/trees/matrices the visualizer needs.

### Q26 — Frontend scope (DECIDED)

**Decision:** The frontend is a native single-page interactive application (SPA). Server-side rendering is not used.

### Q27 — Backend API boundary (DECIDED)

**Decision:** The Go backend exposes a typed JSON API that the frontend calls, querying the collected state from SQLite. The backend owns authentication, collection, storage, and effective-permission resolution; the frontend is a thin read-only viewer.

### Q28 — Repository layout (DECIDED)

**Decision:** Single repository, monorepo layout (e.g. `backend/` and `web/`), so the embedded frontend and binary cannot drift apart. Single-context repo.

### Q29 — Local storage location (DECIDED)

**Decision:** The collected-state SQLite database lives in a per-user data directory (`%LOCALAPPDATA%` on Windows, `~/.local/share/` on Linux), configurable via a flag or environment variable for container volume mounting. It is not placed next to the executable.

### Q30 — Collection execution model (DECIDED)

**Decision:** A single synchronous pass with per-call retry/backoff and throttling awareness. All results are committed in one transaction only at the end. If any required phase fails, the whole run aborts and the previous good run is preserved — honoring "no half data".

### Q31 — Default landing view (DECIDED)

**Decision:** After a successful run, show the run overview, then default to the subject explorer ("What can this user/group access?"). Resource-side and matrix views are one click away.

### Q33 — Testing strategy (DECIDED)

**Decision:** Repeatable unit tests for the resolution/explanation logic plus a snapshot-based integration test against a live or recorded Azure DevOps organization. Correctness of the resolution engine is the top testing priority.

### Q19 — Export format

Does v1 require flat CSV, a native `.xlsx` workbook, or both?

**Recommendation:** Produce a native workbook with sheets for run coverage, effective permissions, explicit assignments, subjects/memberships, and collection warnings. Also allow CSV export of the visible table.

**Decision:** V1 requires CSV export. Native `.xlsx` is not required for V1. Export is per-view: each view produces its own CSV reflecting the current filter and columns (one for the subject/user view, one for the group view, one per pipeline/folder component, etc.).

### Q20 — Executable platforms

Which operating systems and CPU architectures must the bundled executable support? May Azure CLI be a prerequisite?

**Recommendation:** Start with Windows x64 and require Azure CLI to be installed. Detect it and provide actionable login/setup guidance.

**Decision:** Support Linux x64 and Windows x64. Azure CLI is a required prerequisite on both platforms and is not bundled with the application. The app detects it and provides actionable login/setup guidance if missing or unauthenticated.

### Q21 — Local application shell

Should the executable open the default browser to an embedded loopback server or provide a native desktop window?

**Recommendation:** Start a loopback-only web server and open the default browser. Reuse the same frontend and backend in the executable and container distributions.

**Decision:** Start a loopback-only web server and open the default browser. The same frontend and backend are reused in the executable and container distributions.

### Q22 — Container authentication

How should the container access Azure CLI credentials without embedding or copying credentials into the image?

**Recommendation:** Mount a dedicated Azure CLI configuration directory or establish another explicit host-to-container authentication flow. Bind to localhost by default.

**Decision:** Accept the proposal: mount a dedicated Azure CLI configuration directory read-only into the container, never copy credentials into the image, and bind to localhost by default.

### Q23 — Meaning of self-hosted

Does self-hosted mean one administrator runs and accesses the container locally, or may colleagues access it over a network?

**Recommendation:** Define v1 as single-administrator and local/private. Network sharing requires application authentication, TLS, authorization, and concurrency decisions.

**Decision:** V1 is private and local, for a single administrator. Network sharing by colleagues is out of scope.

### Q24 — Proposed v1 acceptance scenario

Confirm or revise this proposed scenario:

> A Windows or Linux administrator authenticated with Azure CLI (a required prerequisite) starts the executable, selects one Azure DevOps organization, collects all accessible projects plus YAML pipeline folders and pipelines, sees progress, and on success examines users', Azure DevOps groups', and Entra groups' effective permissions from either the subject or resource direction, opens an explanation, and exports the current view to CSV.

**Decision:** Accepted with the revisions above (YAML-only, Azure CLI prerequisite, three identity types, per-view CSV export).

## Pending technology and architecture decisions

The interview has not yet selected:

- Visualization model: table, matrix, graph, or coordinated combination (research note below recommends table/tree + scoped matrix + on-demand explanation; not yet accepted).
- CSV export schema and column layout per view.
- Frontend UI framework specifics (React, routing, state, virtualization library).
- Go backend framework/routing and SQLite access layer details.
- Container runtime structure and image build details.

## Resume instructions

Round 2 (Q11–Q24) and the technology round (Q25–Q33) are fully answered and recorded above. The remaining frontier is the **visualization model** (table/tree/matrix/graph coordination) and the **CSV export schema**. Do not begin implementation until the user confirms shared understanding.

## Research note: visualizer information architecture

This research was completed after the interview was paused. It is input to the next round, not an accepted design.

### Recommended product structure

1. **Run overview:** organization, account, capture time, duration, collector version, completeness, coverage, warnings, and actions to explore, export, inspect issues, or run again. Never interpret uncollected data as no access.
2. **Subjects explorer — “What can this user or group access?”:** searchable subjects with a `Project → pipeline folder → pipeline` resource tree and effective-permission columns.
3. **Resources explorer — “Who can access this pipeline or folder?”:** resource tree with a subject table, selected permission, effective result, provenance, and optional group-member expansion.
4. **Scoped matrix:** compare resources and subjects for one permission at a time. Require users to narrow one axis; virtualize both axes. Distinguish Allow, Deny, Not set/no grant, Unknown, and Not collected without relying on color alone.
5. **Permission explanation drawer:** use the tuple `subject × secured resource × permission` as the core unit. Show the verdict and decisive reason first, then contributing membership paths, resource inheritance, raw ACL evidence, and completeness. Use a small node-link graph only for a selected explanation path, accompanied by a textual trace.
6. **Collection issues:** queryable failures showing phase, affected entity, endpoint/error category, retries, and which results may be unknown.

### Visualization recommendation

- Use searchable tables, trees, and scoped matrices for organization-wide inventory and comparison.
- Do not render the entire organization as a node-link graph; it will become an unusable hairball.
- Use node-link/DAG visualization only for a selected permission explanation where path tracing is useful.
- Preserve groups as first-class subjects and expand transitive membership on demand.
- Precompute or index membership closure, resource ancestry, and effective results where dataset size permits; lazy-load detailed explanations.
- Use virtualization, stable sorting, and deterministic result limits for large datasets.

### Collection UX

Suggested phases:

1. Authenticate and validate the organization.
2. Discover projects.
3. Discover pipeline folders and pipelines.
4. Collect identities and direct memberships.
5. Expand nested memberships.
6. Collect ACLs.
7. Resolve effective permissions and explanations.
8. Build indexes and the export model.

Show indeterminate progress while totals are unknown and determinate progress within phases once counts are known. Persist successful data after partial failure. Mark affected results `Unknown` or `Not collected`, never `Deny`.

### Recommended export model

Prefer a normalized `.xlsx` workbook plus equivalent CSV files, containing:

- Manifest/README
- Runs and coverage
- Projects and resources
- Subjects and memberships
- Assignments
- Effective permissions
- Explanation steps
- Collection issues
- A convenient denormalized effective-permissions sheet

Exports should include stable IDs, UTC timestamps, collection completeness, applied filters, explicit unknown values, and spreadsheet formula-injection protection. Very large exports may need to split across sheets or files due to Excel limits.

### Notable edge cases

- Nested groups, multiple membership paths, cycles, and duplicate edges.
- Deleted, disabled, renamed, hidden, or unresolved identities.
- Direct and group assignments producing conflicting contributions.
- Project-, folder-, and pipeline-level assignments and disabled inheritance.
- Duplicate names, moved/deleted pipelines during collection, inaccessible projects, pagination, throttling, token expiry, and partial cancellation.
- New permission bits not yet known to the application.
- Azure DevOps effective results disagreeing with locally reconstructed explanations.
- Broad groups causing an impractical number of fully materialized subject-resource tuples.
- Excel row limits, non-Latin text, locale/encoding issues, accessibility, and high-contrast use.

### Visualizer research sources

- Azure DevOps permissions and groups: https://learn.microsoft.com/en-us/azure/devops/organizations/security/about-permissions?view=azure-devops
- Pipeline security: https://learn.microsoft.com/en-us/azure/devops/pipelines/policies/permissions?view=azure-devops
- ACL query API: https://learn.microsoft.com/en-us/rest/api/azure/devops/security/access-control-lists/query?view=azure-devops-rest-7.1
- Graph membership API: https://learn.microsoft.com/en-us/rest/api/azure/devops/graph/memberships/list?view=azure-devops-rest-7.1
- Graph versus matrix readability study: https://doi.org/10.1109/INFVIS.2004.1
- Shneiderman’s information-seeking overview/filter/details pattern: https://www.cs.umd.edu/~ben/papers/Shneiderman1996eyes.pdf
- Cytoscape.js performance guidance: https://js.cytoscape.org/#performance
- WCAG status messages: https://www.w3.org/WAI/WCAG22/Understanding/status-messages.html
