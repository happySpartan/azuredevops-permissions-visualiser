# Domain glossary

## Azure DevOps administrator

The person accountable for understanding and governing permissions in an enterprise Azure DevOps organization. This may be a platform engineer, DevOps engineer, or developer with delegated administrative responsibility.

## Organization

An Azure DevOps organization containing projects, identities, groups, and secured resources. Organizations are managed by people; they are not application users.

## Subject

A user or group whose permissions are being investigated.

## Effective permission

The permission outcome that applies to a subject for a secured resource after evaluating direct assignments, nested group memberships, inheritance, and Azure DevOps allow/deny precedence.

## Permission explanation

The traceable evidence showing why an effective permission applies, including relevant assignments, group-membership paths, inheritance, and precedence.

## Secured resource

An Azure DevOps entity against which permissions are evaluated. The initial product scope covers pipelines and pipeline folders across multiple projects in one organization.

## Analysis run

A point-in-time collection of identity, group-membership, secured-resource, and permission data from one organization. An Azure DevOps administrator starts a run and explores its collected state after collection completes.

## Collected state

The organization permission data produced by an analysis run and used for navigation and export. It represents Azure DevOps at the time it was collected rather than a continuously synchronized view.

## Pipeline folder

A hierarchy used to organize pipelines within a project and to assign inherited pipeline permissions.
