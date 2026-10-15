import assert from 'node:assert/strict';
import { test } from 'node:test';
import { connectionState, groupEvents, toggleGroup } from '../src/lib/notifications.ts';

const catalog = [
  { kind: 'collection.added', group: 'activity' },
  { kind: 'issue.opened', group: 'attention' },
  { kind: 'release.out', group: 'activity' },
  { kind: 'auth.failed_logins', group: 'security' },
  { kind: 'keys.expiring', group: 'attention' },
];

test('events are listed by group, activity then attention then security, each in catalog order', () => {
  assert.deepEqual(groupEvents(catalog), [
    { group: 'activity', kinds: ['collection.added', 'release.out'] },
    { group: 'attention', kinds: ['issue.opened', 'keys.expiring'] },
    { group: 'security', kinds: ['auth.failed_logins'] },
  ]);
});

test('a group the UI does not know yet comes after the known ones, and empty groups are left out', () => {
  assert.deepEqual(groupEvents([{ kind: 'x.new', group: 'future' }, { kind: 'auth.failed_logins', group: 'security' }]), [
    { group: 'security', kinds: ['auth.failed_logins'] },
    { group: 'future', kinds: ['x.new'] },
  ]);
  assert.deepEqual(groupEvents([]), []);
});

test('"All" of a group adds its missing kinds or removes all of them, keeping the others', () => {
  assert.deepEqual(toggleGroup(['issue.opened', 'collection.added'], ['collection.added', 'release.out'], true),
    ['issue.opened', 'collection.added', 'release.out']);
  assert.deepEqual(toggleGroup(['issue.opened', 'collection.added'], ['collection.added', 'release.out'], false), ['issue.opened']);
  assert.deepEqual(toggleGroup([], ['a'], false), []);
});

test("a connection's state: paused wins (health reports it), then off, then untested until something was delivered", () => {
  assert.equal(connectionState({ enabled: true, pausedReason: 'the token was rejected' }, { state: 'sent' }), 'paused');
  assert.equal(connectionState({ enabled: false, pausedReason: 'the token was rejected' }), 'paused');
  assert.equal(connectionState({ enabled: false, pausedReason: '' }, { state: 'sent' }), 'off');
  assert.equal(connectionState({ enabled: true, pausedReason: '' }), 'untested');
  assert.equal(connectionState({ enabled: true, pausedReason: '' }, { state: 'sent' }), 'working');
  assert.equal(connectionState({ enabled: true, pausedReason: '' }, { state: 'queued' }), 'working');
});
