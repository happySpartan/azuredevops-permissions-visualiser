import { useEffect, useState } from 'react'
import PermissionExplanationDrawer from './PermissionExplanationDrawer'

export interface Subject {
  descriptor: string
  displayName: string
  origin: string
  kind: string
}

export interface PermissionResult {
  bit: number
  name: string
  displayName: string
  state: 'allow' | 'deny' | 'notSet'
  direct: boolean
  inherited: boolean
  viaGroup: boolean
}

export interface PermissionResource {
  token: string
  type: string
  name: string
  projectName: string
  path: string
  permissions: PermissionResult[]
}

interface SubjectPermissionDetail {
  subject: Subject
  resources: PermissionResource[]
}

interface Props {
  subject: Subject
  onBack: () => void
}

export default function SubjectPermissions({ subject, onBack }: Props) {
  const [detail, setDetail] = useState<SubjectPermissionDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showAll, setShowAll] = useState(false)
  const [explainTarget, setExplainTarget] = useState<{ resource: PermissionResource; permission: PermissionResult } | null>(null)
  const prominent = new Set(['ViewBuilds', 'QueueBuilds', 'ViewBuildDefinition', 'EditBuildDefinition', 'DeleteBuildDefinition', 'AdministerBuildPermissions'])

  useEffect(() => {
    const query = new URLSearchParams({ descriptor: subject.descriptor })
    fetch(`/api/explorer/subjects/permissions?${query}`)
      .then(async (response) => {
        const body = await response.json()
        if (!response.ok) throw new Error(body.error ?? `Request failed (${response.status})`)
        return body as SubjectPermissionDetail
      })
      .then(setDetail)
      .catch((caught) => setError(caught instanceof Error ? caught.message : String(caught)))
      .finally(() => setLoading(false))
  }, [subject.descriptor])

  return (
    <section>
      <button className="back-link" onClick={onBack}>← Back to subjects</button>
      <div className="page-heading subject-heading">
        <div><p className="eyebrow">Subject permissions</p><h1>{subject.displayName}</h1><p>{subject.kind || 'Unknown type'} · {subject.origin || 'Unknown origin'}</p></div>
        <button className="button secondary" onClick={() => setShowAll(!showAll)}>{showAll ? 'Show key actions' : 'Show all actions'}</button>
      </div>
      <div className="source-note"><strong>Azure DevOps result</strong><span>Effective states are the inherited and effective masks reported by Azure DevOps, not a locally reconstructed verdict.</span></div>
      {loading && <div className="panel loading-state">Loading collected permissions…</div>}
      {error && <p className="inline-error">{error}</p>}
      {detail && detail.resources.length === 0 && <div className="panel table-empty">No collected access-control entry was returned for this subject.</div>}
      <div className="permission-resources">
        {detail?.resources.map((resource) => {
          const permissions = showAll ? resource.permissions : resource.permissions.filter((permission) => prominent.has(permission.name))
          return <article className="panel permission-card" key={resource.token}>
            <header><div><span className="resource-type">{resource.type}</span><h2>{resource.name}</h2><p>{resource.projectName}{resource.path ? ` · ${resource.path}` : ''}</p></div><code>{resource.token}</code></header>
            <div className="permission-grid">
              {permissions.map((permission) => <div className="permission-row" key={permission.bit} role="button" tabIndex={0} onClick={() => setExplainTarget({ resource, permission })} onKeyDown={(event) => { if (event.key === 'Enter') setExplainTarget({ resource, permission }) }}>
                <div><strong>{permission.displayName || permission.name}</strong><span>{permission.name}</span></div>
                <span className={`permission-state ${permission.state}`}>{permission.state === 'notSet' ? 'Not set' : permission.state}</span>
                <div className="provenance">{permission.direct && <span>Direct</span>}{permission.inherited && <span>Inherited</span>}{permission.viaGroup && <span>Via group</span>}{!permission.direct && !permission.inherited && !permission.viaGroup && <span>—</span>}</div>
              </div>)}
            </div>
          </article>
        })}
      </div>
      {explainTarget && <PermissionExplanationDrawer subject={subject} resource={explainTarget.resource} permission={explainTarget.permission} onClose={() => setExplainTarget(null)} />}
    </section>
  )
}
