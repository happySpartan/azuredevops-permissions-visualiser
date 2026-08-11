import assert from 'node:assert/strict'
import test from 'node:test'
import { membershipSummary, type GroupMember } from './GroupMembership.ts'

const members: GroupMember[] = [
  {
    subject: { descriptor: 'g-platform', displayName: 'Platform', origin: 'aad', kind: 'group' },
    direct: true,
    path: [
      { descriptor: 'g-root', displayName: 'Engineering', origin: 'aad', kind: 'group' },
      { descriptor: 'g-platform', displayName: 'Platform', origin: 'aad', kind: 'group' },
    ],
  },
  {
    subject: { descriptor: 'u-alice', displayName: 'Alice', origin: 'aad', kind: 'user' },
    direct: false,
    path: [
      { descriptor: 'g-root', displayName: 'Engineering', origin: 'aad', kind: 'group' },
      { descriptor: 'g-platform', displayName: 'Platform', origin: 'aad', kind: 'group' },
      { descriptor: 'u-alice', displayName: 'Alice', origin: 'aad', kind: 'user' },
    ],
  },
]

test('membershipSummary separates direct and transitive members while retaining groups', () => {
  assert.deepEqual(membershipSummary(members), {
    direct: 1,
    transitive: 1,
    users: 1,
    groups: 1,
  })
})
