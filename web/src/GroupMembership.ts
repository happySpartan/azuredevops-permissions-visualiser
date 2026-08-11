export interface Subject {
  descriptor: string
  displayName: string
  origin: string
  kind: string
}

export interface GroupMember {
  subject: Subject
  direct: boolean
  path: Subject[]
}

export interface GroupMembershipDetail {
  group: Subject
  members: GroupMember[]
}

export interface MembershipSummary {
  direct: number
  transitive: number
  users: number
  groups: number
}

export function membershipSummary(members: GroupMember[]): MembershipSummary {
  return members.reduce<MembershipSummary>((summary, member) => {
    if (member.direct) summary.direct += 1
    else summary.transitive += 1
    if (member.subject.kind === 'group') summary.groups += 1
    else if (member.subject.kind === 'user') summary.users += 1
    return summary
  }, { direct: 0, transitive: 0, users: 0, groups: 0 })
}
