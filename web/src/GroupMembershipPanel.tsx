import { useEffect, useState } from 'react'
import { membershipSummary, type GroupMembershipDetail, type Subject } from './GroupMembership'

interface Props {
  group: Subject
}

export default function GroupMembershipPanel({ group }: Props) {
  const [detail, setDetail] = useState<GroupMembershipDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setLoading(true)
    setError(null)
    const query = new URLSearchParams({ descriptor: group.descriptor })
    fetch(`/api/explorer/groups/memberships?${query}`)
      .then(async (response) => {
        const body = await response.json()
        if (!response.ok) throw new Error(body.error ?? `Request failed (${response.status})`)
        return body as GroupMembershipDetail
      })
      .then(setDetail)
      .catch((caught) => setError(caught instanceof Error ? caught.message : String(caught)))
      .finally(() => setLoading(false))
  }, [group.descriptor])

  const summary = detail ? membershipSummary(detail.members) : null
  const exportURL = `/api/explorer/groups/memberships/export?${new URLSearchParams({ descriptor: group.descriptor })}`

  return (
    <section className="membership-section" aria-labelledby="membership-heading">
      <div className="section-heading">
        <div>
          <p className="eyebrow">Group membership</p>
          <h2 id="membership-heading">Direct and transitive members</h2>
          <p>Nested groups remain visible as subjects. Each member includes the collected membership path.</p>
        </div>
        <a className="button secondary" href={exportURL} download>Export membership CSV</a>
      </div>
      {loading && <div className="panel loading-state">Loading collected group membership…</div>}
      {error && <p className="inline-error">{error}</p>}
      {detail && summary && (
        <>
          <div className="membership-summary" aria-label="Membership summary">
            <span><strong>{summary.direct}</strong> direct</span>
            <span><strong>{summary.transitive}</strong> transitive</span>
            <span><strong>{summary.users}</strong> users</span>
            <span><strong>{summary.groups}</strong> groups</span>
          </div>
          <div className="panel table-panel membership-table">
            <table>
              <thead><tr><th>Member</th><th>Type</th><th>Relationship</th><th>Membership path</th></tr></thead>
              <tbody>
                {detail.members.map((member) => (
                  <tr key={member.subject.descriptor}>
                    <td><strong>{member.subject.displayName}</strong><small>{member.subject.descriptor}</small></td>
                    <td><span className="kind-badge">{member.subject.kind || 'unknown'}</span></td>
                    <td><span className={member.direct ? 'relationship-badge direct' : 'relationship-badge transitive'}>{member.direct ? 'Direct' : 'Transitive'}</span></td>
                    <td><div className="membership-path">{member.path.map((subject, index) => <span key={`${subject.descriptor}-${index}`}>{index > 0 && <b>›</b>}<em>{subject.displayName}</em></span>)}</div></td>
                  </tr>
                ))}
              </tbody>
            </table>
            {detail.members.length === 0 && <div className="table-empty">No direct or transitive members were collected for this group.</div>}
          </div>
        </>
      )}
    </section>
  )
}
