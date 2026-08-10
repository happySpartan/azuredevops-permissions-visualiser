import { useCallback, useEffect, useState } from 'react'
import './app.css'
import SubjectPermissions from './SubjectPermissions'

interface Run {
  ID: number
  Org: string
  Status: string
  StartedAt: string
  CompletedAt: string | null
  ProjectCount: number
  FolderCount: number
  PipelineCount: number
  SubjectCount: number
  AssignmentCount: number
}

interface Subject {
  descriptor: string
  displayName: string
  origin: string
  kind: string
}

interface SubjectPage {
  items: Subject[]
  total: number
  limit: number
  offset: number
}

interface Pipeline {
  id: number
  name: string
  folderPath: string
  queueStatus: string
}

interface Project {
  id: string
  name: string
  folders: { path: string }[]
  pipelines: Pipeline[]
}

type View = 'overview' | 'subjects' | 'resources'

async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init)
  const body = await response.json()
  if (!response.ok) throw new Error(body.error ?? `Request failed (${response.status})`)
  return body as T
}

function App() {
  const [run, setRun] = useState<Run | null>(null)
  const [view, setView] = useState<View>('overview')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadRun = useCallback(async () => {
    try {
      const response = await api<{ run: Run | null }>('/api/run/current')
      setRun(response.run)
      setError(null)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught))
    }
  }, [])

  useEffect(() => { void loadRun() }, [loadRun])

  async function collect() {
    setBusy(true)
    setError(null)
    try {
      await api('/api/run/collect', { method: 'POST' })
      await loadRun()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught))
    } finally {
      setBusy(false)
    }
  }

  async function deleteData() {
    if (!window.confirm('Delete all locally collected permission data?')) return
    setBusy(true)
    try {
      await api('/api/run/delete', { method: 'POST' })
      setRun(null)
      setView('overview')
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand-mark" aria-hidden="true">A</div>
        <div className="brand-copy">
          <strong>Permissions Visualiser</strong>
          <span>Azure DevOps</span>
        </div>
        {run && <div className="org-pill"><span className="status-dot" />{run.Org}</div>}
        <div className="topbar-actions">
          <button className="button secondary" disabled={busy} onClick={() => void collect()}>
            {busy ? 'Collecting…' : run ? 'Run again' : 'Collect organization'}
          </button>
        </div>
      </header>

      <div className="body-grid">
        <nav className="sidebar" aria-label="Main navigation">
          <button className={view === 'overview' ? 'nav-item active' : 'nav-item'} onClick={() => setView('overview')}>
            <span aria-hidden="true">◫</span> Overview
          </button>
          <p className="nav-heading">Explore</p>
          <button disabled={!run} className={view === 'subjects' ? 'nav-item active' : 'nav-item'} onClick={() => setView('subjects')}>
            <span aria-hidden="true">◎</span> Subjects
          </button>
          <button disabled={!run} className={view === 'resources' ? 'nav-item active' : 'nav-item'} onClick={() => setView('resources')}>
            <span aria-hidden="true">◇</span> Resources
          </button>
          <div className="sidebar-spacer" />
          <div className="local-note"><strong>Local only</strong><span>Collected data stays on this device.</span></div>
        </nav>

        <main className="content">
          {error && <div className="error-banner" role="alert"><strong>Unable to complete request</strong><span>{error}</span><button onClick={() => setError(null)}>×</button></div>}
          {view === 'overview' && <Overview run={run} busy={busy} onCollect={collect} onDelete={deleteData} onExplore={() => setView('subjects')} />}
          {view === 'subjects' && run && <Subjects />}
          {view === 'resources' && run && <Resources />}
        </main>
      </div>
    </div>
  )
}

function Overview({ run, busy, onCollect, onDelete, onExplore }: {
  run: Run | null
  busy: boolean
  onCollect: () => void
  onDelete: () => void
  onExplore: () => void
}) {
  if (!run) {
    return (
      <section className="empty-state">
        <div className="empty-icon">↻</div>
        <p className="eyebrow">Point-in-time analysis</p>
        <h1>Understand who can do what</h1>
        <p>Collect pipeline and pipeline-folder permissions from one Azure DevOps organization, then explore the result locally.</p>
        <button className="button primary" disabled={busy} onClick={onCollect}>{busy ? 'Collecting…' : 'Collect organization'}</button>
        <small>Requires AZDO_ORG and an authenticated Azure CLI.</small>
      </section>
    )
  }

  const captured = run.CompletedAt ? new Date(run.CompletedAt).toLocaleString() : 'Unknown'
  const stats = [
    ['Projects', run.ProjectCount],
    ['Pipeline folders', run.FolderCount],
    ['Pipelines', run.PipelineCount],
    ['Subjects', run.SubjectCount],
    ['Assignments', run.AssignmentCount],
  ]

  return (
    <section>
      <div className="page-heading">
        <div><p className="eyebrow">Latest analysis run</p><h1>{run.Org}</h1><p>State captured {captured}. Azure DevOps may have changed since.</p></div>
        <button className="button primary" onClick={onExplore}>Explore access</button>
      </div>
      <div className="status-card">
        <div className="success-icon">✓</div>
        <div><strong>Collection complete</strong><p>All required data was collected successfully.</p></div>
        <span className="complete-badge">Complete</span>
      </div>
      <div className="stats-grid">
        {stats.map(([label, value]) => <article className="stat-card" key={label}><span>{label}</span><strong>{value.toLocaleString()}</strong></article>)}
      </div>
      <div className="export-actions">
        <span>Export collected data</span>
        <a className="button secondary" href="/api/run/export/effective-permissions" download>Effective permissions (CSV)</a>
        <a className="button secondary" href="/api/run/export/assignments" download>Raw assignments (CSV)</a>
      </div>
      <div className="panel run-details">
        <div><h2>Run details</h2><p>This collected state is retained until the next successful run.</p></div>
        <dl><div><dt>Organization</dt><dd>{run.Org}</dd></div><div><dt>Run ID</dt><dd>#{run.ID}</dd></div><div><dt>Status</dt><dd>{run.Status}</dd></div></dl>
        <button className="danger-link" onClick={onDelete}>Delete collected data</button>
      </div>
    </section>
  )
}

function Subjects() {
  const [page, setPage] = useState<SubjectPage | null>(null)
  const [search, setSearch] = useState('')
  const [kind, setKind] = useState('')
  const [offset, setOffset] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState<Subject | null>(null)

  useEffect(() => {
    const timer = window.setTimeout(() => {
      const query = new URLSearchParams({ search, kind, limit: '50', offset: String(offset) })
      api<SubjectPage>(`/api/explorer/subjects?${query}`).then(setPage).catch((caught) => setError(String(caught)))
    }, 200)
    return () => window.clearTimeout(timer)
  }, [search, kind, offset])

  if (selected) {
    return <SubjectPermissions subject={selected} onBack={() => setSelected(null)} />
  }

  return (
    <section>
      <div className="page-heading"><div><p className="eyebrow">Access explorer</p><h1>Subjects</h1><p>Start with a user or group to investigate its collected access.</p></div></div>
      <div className="toolbar panel">
        <label className="search-field"><span>Search</span><input value={search} onChange={(event) => { setSearch(event.target.value); setOffset(0) }} placeholder="Name or descriptor" /></label>
        <label><span>Type</span><select value={kind} onChange={(event) => { setKind(event.target.value); setOffset(0) }}><option value="">All subjects</option><option value="user">Users</option><option value="group">Groups</option></select></label>
      </div>
      {error && <p className="inline-error">{error}</p>}
      <div className="panel table-panel">
        <table><thead><tr><th>Subject</th><th>Type</th><th>Origin</th><th>Descriptor</th></tr></thead>
          <tbody>{page?.items.map((subject) => <tr className="selectable-row" key={subject.descriptor} tabIndex={0} onClick={() => setSelected(subject)} onKeyDown={(event) => { if (event.key === 'Enter') setSelected(subject) }}><td><strong>{subject.displayName}</strong></td><td><span className="kind-badge">{subject.kind || 'unknown'}</span></td><td>{subject.origin || '—'}</td><td className="descriptor">{subject.descriptor}</td></tr>)}</tbody>
        </table>
        {page && page.total === 0 && <div className="table-empty">No subjects match these filters.</div>}
        {page && <div className="pagination"><span>{page.total === 0 ? 0 : page.offset + 1}–{Math.min(page.offset + page.items.length, page.total)} of {page.total}</span><div><button disabled={page.offset === 0} onClick={() => setOffset(Math.max(0, offset - 50))}>Previous</button><button disabled={page.offset + page.limit >= page.total} onClick={() => setOffset(offset + 50)}>Next</button></div></div>}
      </div>
    </section>
  )
}

function Resources() {
  const [projects, setProjects] = useState<Project[]>([])
  const [error, setError] = useState<string | null>(null)
  useEffect(() => { api<{ projects: Project[] }>('/api/explorer/resources').then((data) => setProjects(data.projects)).catch((caught) => setError(String(caught))) }, [])

  return (
    <section>
      <div className="page-heading"><div><p className="eyebrow">Access explorer</p><h1>Resources</h1><p>Browse the collected project, pipeline-folder, and YAML pipeline hierarchy.</p></div></div>
      {error && <p className="inline-error">{error}</p>}
      <div className="resource-list">
        {projects.map((project) => <details className="panel project-card" open key={project.id}><summary><div><strong>{project.name}</strong><span>{project.folders.length} folders · {project.pipelines.length} pipelines</span></div><span className="project-badge">Project</span></summary>
          <div className="resource-grid">
            {project.folders.map((folder) => <div className="resource-row" key={`folder-${folder.path}`}><span className="resource-icon folder">▱</span><div><strong>{folder.path === '/' ? 'Root folder' : folder.path}</strong><span>Pipeline folder</span></div></div>)}
            {project.pipelines.map((pipeline) => <div className="resource-row" key={`pipeline-${pipeline.id}`}><span className="resource-icon pipeline">▷</span><div><strong>{pipeline.name}</strong><span>{pipeline.folderPath || '/'} · {pipeline.queueStatus || 'status unknown'}</span></div></div>)}
            {project.folders.length + project.pipelines.length === 0 && <p className="table-empty">No pipeline resources were collected for this project.</p>}
          </div>
        </details>)}
      </div>
    </section>
  )
}

export default App
