import { useEffect, useState } from 'react'
import type { PermissionResource, PermissionResult, Subject } from './SubjectPermissions'
import PermissionExplanationDrawer from './PermissionExplanationDrawer'

interface Project { id: string; name: string }
interface MatrixAction { bit: number; name: string; displayName: string }
interface MatrixRow { resource: PermissionResource; cells: Record<string, PermissionResult | undefined> }
interface Matrix { projectId: string; projectName: string; action: MatrixAction; subjects: Subject[]; rows: MatrixRow[] }

const actions: MatrixAction[] = [
  { bit: 1, name: 'ViewBuilds', displayName: 'View builds' },
  { bit: 2, name: 'EditBuildQuality', displayName: 'Edit build quality' },
  { bit: 4, name: 'RetainIndefinitely', displayName: 'Retain indefinitely' },
  { bit: 8, name: 'DeleteBuilds', displayName: 'Delete builds' },
  { bit: 16, name: 'ManageBuildQualities', displayName: 'Manage build qualities' },
  { bit: 32, name: 'DestroyBuilds', displayName: 'Destroy builds' },
  { bit: 64, name: 'UpdateBuildInformation', displayName: 'Update build information' },
  { bit: 128, name: 'QueueBuilds', displayName: 'Queue builds' },
  { bit: 256, name: 'ManageBuildQueue', displayName: 'Manage build queue' },
  { bit: 512, name: 'StopBuilds', displayName: 'Stop builds' },
  { bit: 1024, name: 'ViewBuildDefinition', displayName: 'View build definition' },
  { bit: 2048, name: 'EditBuildDefinition', displayName: 'Edit build definition' },
  { bit: 4096, name: 'DeleteBuildDefinition', displayName: 'Delete build definition' },
  { bit: 8192, name: 'OverrideBuildCheckInValidation', displayName: 'Override check-in validation' },
  { bit: 16384, name: 'AdministerBuildPermissions', displayName: 'Administer build permissions' },
]

export default function PermissionMatrix() {
  const [projects, setProjects] = useState<Project[]>([])
  const [projectId, setProjectId] = useState('')
  const [bit, setBit] = useState(1)
  const [matrix, setMatrix] = useState<Matrix | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [target, setTarget] = useState<{ subject: Subject; resource: PermissionResource; permission: PermissionResult } | null>(null)

  useEffect(() => {
    fetch('/api/explorer/resources').then(async (response) => {
      const body = await response.json()
      if (!response.ok) throw new Error(body.error ?? `Request failed (${response.status})`)
      return body.projects as Project[]
    }).then((items) => { setProjects(items); if (items.length) setProjectId(items[0].id) }).catch((caught) => setError(String(caught)))
  }, [])

  useEffect(() => {
    if (!projectId) return
    setMatrix(null)
    setError(null)
    const query = new URLSearchParams({ projectId, bit: String(bit) })
    fetch(`/api/explorer/matrix?${query}`).then(async (response) => {
      const body = await response.json()
      if (!response.ok) throw new Error(body.error ?? `Request failed (${response.status})`)
      return body as Matrix
    }).then(setMatrix).catch((caught) => setError(caught instanceof Error ? caught.message : String(caught)))
  }, [projectId, bit])

  return <section>
    <div className="page-heading"><div><p className="eyebrow">Scoped comparison</p><h1>Permission matrix</h1><p>Compare one Build permission across subjects and secured resources in one project.</p></div>{projectId && <a className="button secondary" href={`/api/explorer/matrix/export?${new URLSearchParams({ projectId, bit: String(bit) })}`} download>Export CSV</a>}</div>
    <div className="toolbar panel matrix-toolbar">
      <label><span>Project</span><select value={projectId} onChange={(event) => setProjectId(event.target.value)}>{projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label>
      <label><span>Permission</span><select value={bit} onChange={(event) => setBit(Number(event.target.value))}>{actions.map((action) => <option key={action.bit} value={action.bit}>{action.displayName}</option>)}</select></label>
    </div>
    <div className="matrix-legend"><span className="matrix-cell allow">Allow</span><span className="matrix-cell deny">Deny</span><span className="matrix-cell notSet">Not set</span><span className="matrix-cell unknown">No collected assignment</span></div>
    {error && <p className="inline-error">{error}</p>}
    {!error && !matrix && <div className="panel loading-state">Loading scoped matrix…</div>}
    {matrix && <div className="panel matrix-scroll"><table className="permission-matrix"><thead><tr><th className="matrix-resource-column">Secured resource</th>{matrix.subjects.map((subject) => <th key={subject.descriptor}><span>{subject.displayName || subject.descriptor}</span><small>{subject.kind}</small></th>)}</tr></thead><tbody>{matrix.rows.map((row) => <tr key={row.resource.token}><th><strong>{row.resource.name}</strong><small>{row.resource.type} · {row.resource.path || '/'}</small></th>{matrix.subjects.map((subject) => {
      const cell = row.cells[subject.descriptor]
      return <td key={subject.descriptor}>{cell ? <button className={`matrix-cell ${cell.state}`} title={`${cell.state}${cell.direct ? ' · Direct' : cell.inherited ? ' · Inherited' : cell.viaGroup ? ' · Via group' : ''}`} onClick={() => setTarget({ subject, resource: row.resource, permission: cell })}><span>{cell.state === 'notSet' ? '—' : cell.state === 'allow' ? '✓' : '×'}</span><small>{cell.direct ? 'Direct' : cell.inherited ? 'Inherited' : cell.viaGroup ? 'Via group' : 'Not set'}</small></button> : <span className="matrix-cell unknown" title="No collected assignment"><span>?</span><small>Unknown</small></span>}</td>
    })}</tr>)}</tbody></table>{matrix.rows.length === 0 && <div className="table-empty">No assignments were collected in this project.</div>}</div>}
    {target && <PermissionExplanationDrawer subject={target.subject} resource={target.resource} permission={target.permission} onClose={() => setTarget(null)} />}
  </section>
}
