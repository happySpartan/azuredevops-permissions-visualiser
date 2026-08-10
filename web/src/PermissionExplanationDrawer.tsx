import { useEffect, useState } from 'react'
import type { PermissionResource, PermissionResult, Subject } from './SubjectPermissions'

interface Evidence {
  token: string
  resource: PermissionResource
  subject: Subject
  state: 'allow' | 'deny' | 'notSet'
  explicitAllow: boolean
  explicitDeny: boolean
  effectiveAllow: boolean
  effectiveDeny: boolean
  fromAncestor: boolean
  viaGroup: boolean
  membershipPath: Subject[]
}

interface Explanation {
  subject: Subject
  resource: PermissionResource
  permission: PermissionResult
  state: 'allow' | 'deny' | 'notSet'
  resourcePath: PermissionResource[]
  evidence: Evidence[]
}

interface Props {
  subject: Subject
  resource: PermissionResource
  permission: PermissionResult
  onClose: () => void
}

export default function PermissionExplanationDrawer({ subject, resource, permission, onClose }: Props) {
  const [explanation, setExplanation] = useState<Explanation | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const query = new URLSearchParams({ descriptor: subject.descriptor, token: resource.token, bit: String(permission.bit) })
    fetch(`/api/explorer/subjects/explanation?${query}`)
      .then(async (response) => {
        const body = await response.json()
        if (!response.ok) throw new Error(body.error ?? `Request failed (${response.status})`)
        return body as Explanation
      })
      .then(setExplanation)
      .catch((caught) => setError(caught instanceof Error ? caught.message : String(caught)))
  }, [permission.bit, resource.token, subject.descriptor])

  return <div className="drawer-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <aside className="explanation-drawer" role="dialog" aria-modal="true" aria-labelledby="explanation-title">
      <header className="drawer-header"><div><p className="eyebrow">Permission explanation</p><h2 id="explanation-title">{permission.displayName || permission.name}</h2><p>{subject.displayName} on {resource.name}</p></div><button className="drawer-close" aria-label="Close explanation" onClick={onClose}>×</button></header>
      {error && <p className="inline-error drawer-message">{error}</p>}
      {!error && !explanation && <p className="drawer-message">Loading evidence…</p>}
      {explanation && <div className="drawer-content">
        <section className={`verdict-card ${explanation.state}`}><span>Azure DevOps effective result</span><strong>{explanation.state === 'notSet' ? 'Not set' : explanation.state}</strong><p>This verdict comes from the effective masks returned by Azure DevOps.</p></section>

        <section className="explanation-section"><h3>Resource inheritance path</h3><div className="trace-path">{explanation.resourcePath.map((item, index) => <div className="trace-node" key={item.token}><span>{index + 1}</span><div><strong>{item.name}</strong><small>{item.type} · {item.token}</small></div></div>)}</div></section>

        <section className="explanation-section"><h3>Contributing assignments</h3>{explanation.evidence.length === 0 ? <p>No explicit contributing ACE was found in the collected ancestry. The effective result remains Azure DevOps' reported value.</p> : <div className="evidence-list">{explanation.evidence.map((item, index) => <article className="evidence-card" key={`${item.token}-${item.subject.descriptor}-${index}`}>
          <div className="evidence-title"><span className={`permission-state ${item.state}`}>{item.state}</span><strong>{item.subject.displayName || item.subject.descriptor}</strong></div>
          <dl><div><dt>Assigned at</dt><dd>{item.resource.name}</dd></div><div><dt>Raw ACE</dt><dd>{item.explicitDeny ? 'Explicit deny' : 'Explicit allow'}</dd></div><div><dt>Scope</dt><dd>{item.fromAncestor ? 'Ancestor resource' : 'Selected resource'}</dd></div></dl>
          {item.viaGroup && <div className="membership-trace"><span>Membership path</span><div>{item.membershipPath.map((member, pathIndex) => <span key={member.descriptor}>{pathIndex > 0 && <b>→</b>}<em>{member.displayName || member.descriptor}</em></span>)}</div></div>}
          <details className="raw-evidence"><summary>Raw evidence</summary><code>token={item.token}<br />descriptor={item.subject.descriptor}<br />explicitAllow={String(item.explicitAllow)} explicitDeny={String(item.explicitDeny)}<br />effectiveAllow={String(item.effectiveAllow)} effectiveDeny={String(item.effectiveDeny)}</code></details>
        </article>)}</div>}</section>
      </div>}
    </aside>
  </div>
}
