import { useEffect, useState } from 'react'
import type { Subject } from './SubjectPermissions'
import type { PermissionResult, PermissionResource } from './SubjectPermissions'
import PermissionExplanationDrawer from './PermissionExplanationDrawer'

export interface SubjectEntry {
  subject: Subject
  permissions: PermissionResult[]
}

interface ResourcePermissionDetail {
  resource: PermissionResource
  subjects: SubjectEntry[]
}

interface Props {
  resource: { token: string; name: string; type: string; path: string }
  projectName: string
  onBack: () => void
}

export default function ResourcePermissions({ resource, projectName, onBack }: Props) {
  const [detail, setDetail] = useState<ResourcePermissionDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showAll, setShowAll] = useState(false)
  const [explainTarget, setExplainTarget] = useState<{ subject: Subject; permission: PermissionResult } | null>(null)
  const prominent = new Set(['ViewBuilds', 'QueueBuilds', 'ViewBuildDefinition', 'EditBuildDefinition', 'DeleteBuildDefinition', 'AdministerBuildPermissions'])

  useEffect(() => {
    const query = new URLSearchParams({ token: resource.token })
    fetch(`/api/explorer/resources/permissions?${query}`)
      .then(async (response) => {
        const body = await response.json()
        if (!response.ok) throw new Error(body.error ?? `Request failed (${response.status})`)
        return body as ResourcePermissionDetail
      })
      .then(setDetail)
      .catch((caught) => setError(caught instanceof Error ? caught.message : String(caught)))
      .finally(() => setLoading(false))
  }, [resource.token])

  return (
    <section>
      <button className="back-link" onClick={onBack}>← Back to resources</button>
      <div className="page-heading subject-heading">
        <div>
          <p className="eyebrow">{resource.type} permissions</p>
          <h1>{resource.name}</h1>
          <p>{projectName}{resource.path ? ` · ${resource.path}` : ''}</p>
        </div>
        <div className="heading-actions">
          <a className="button secondary" href={`/api/explorer/resources/export?${new URLSearchParams({ token: resource.token })}`} download>Export CSV</a>
          <button className="button secondary" onClick={() => setShowAll(!showAll)}>{showAll ? 'Show key actions' : 'Show all actions'}</button>
        </div>
      </div>
      <div className="source-note"><strong>Azure DevOps result</strong><span>Effective states are the inherited and effective masks reported by Azure DevOps, not a locally reconstructed verdict.</span></div>
      {loading && <div className="panel loading-state">Loading subjects with permissions…</div>}
      {error && <p className="inline-error">{error}</p>}
      {detail && detail.subjects.length === 0 && <div className="panel table-empty">No subjects have assignments on this resource.</div>}
      <div className="permission-resources">
        {detail?.subjects.map((entry) => {
          const permissions = showAll ? entry.permissions : entry.permissions.filter((p) => prominent.has(p.name))
          return <article className="panel permission-card" key={entry.subject.descriptor}>
            <header>
              <div>
                <span className="resource-type">{entry.subject.kind}</span>
                <h2>{entry.subject.displayName || entry.subject.descriptor}</h2>
                <p>{entry.subject.origin ? entry.subject.origin : 'Unknown origin'}</p>
              </div>
              <code>{entry.subject.descriptor}</code>
            </header>
            <div className="permission-grid">
              {permissions.map((permission) => (
                <div className="permission-row" key={permission.bit} role="button" tabIndex={0}
                  onClick={() => setExplainTarget({ subject: entry.subject, permission })}
                  onKeyDown={(event) => { if (event.key === 'Enter') setExplainTarget({ subject: entry.subject, permission }) }}>
                  <div><strong>{permission.displayName || permission.name}</strong><span>{permission.name}</span></div>
                  <span className={`permission-state ${permission.state}`}>{permission.state === 'notSet' ? 'Not set' : permission.state}</span>
                  <div className="provenance">{permission.direct && <span>Direct</span>}{permission.inherited && <span>Inherited</span>}{permission.viaGroup && <span>Via group</span>}{!permission.direct && !permission.inherited && !permission.viaGroup && <span>—</span>}</div>
                </div>
              ))}
            </div>
          </article>
        })}
      </div>
      {explainTarget && detail && (
        <PermissionExplanationDrawer
          subject={explainTarget.subject}
          resource={detail.resource}
          permission={explainTarget.permission}
          onClose={() => setExplainTarget(null)}
        />
      )}
    </section>
  )
}
