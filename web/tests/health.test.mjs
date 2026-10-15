import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { dotLabel, issueHref, issueTitle, reasonText, sinceText, worstLevel } from '../src/lib/health.ts';
import { parseHash } from '../src/lib/route.ts';

const t = (key, opts) => `${key}${opts ? ' ' + JSON.stringify(opts) : ''}`;
const now = new Date('2026-10-11T12:00:00Z');
const ago = (ms) => new Date(now.getTime() - ms);
const day = 24 * 3600 * 1000;

test('the worst level decides the sidebar dot', () => {
  assert.equal(worstLevel([]), null);
  assert.equal(worstLevel([{ level: 'warning' }]), 'warning');
  assert.equal(worstLevel([{ level: 'warning' }, { level: 'error' }]), 'error');
});

test('the dot says which page and how bad, translated', () => {
  assert.equal(dotLabel('error', 'System', t), 'health.dot {"page":"System","level":"health.error"}');
});

test('an issue links to where it can be fixed: the source itself, the backups card', () => {
  assert.equal(issueHref({ target: 'providers', targetId: '' }), '#/providers');
  assert.equal(issueHref({ target: 'sources', targetId: 's 1' }), '#/sources/s%201');
  assert.equal(issueHref({ target: 'system/backups', targetId: '' }), '#/system/backups');
  assert.equal(issueHref({ target: 'notifications', targetId: 'c-1' }), '#/notifications');
  assert.equal(issueHref({ target: '', targetId: '' }), null);
  assert.equal(issueHref({ target: 'somewhere-new', targetId: '' }), null);
  assert.equal(parseHash(issueHref({ target: 'sources', targetId: 's 1' })).target, 's 1', 'the page opens what the link names');
  assert.equal(parseHash(issueHref({ target: 'system/backups', targetId: '' })).target, 'backups');
});

test('the backups and the disk are named in the UI language, providers and sources by their name', () => {
  assert.equal(issueTitle({ kind: 'backups', source: '' }, t), 'health.kinds.backups');
  assert.equal(issueTitle({ kind: 'disk', source: '' }, t), 'health.kinds.disk');
  assert.equal(issueTitle({ kind: 'source', source: 'My store' }, t), 'My store');
  assert.equal(issueTitle({ kind: 'connector', source: 'My phone' }, t), 'My phone');
});

test('the reason is translated with its values; an unknown one shows the server message', () => {
  assert.equal(reasonText({ reason: 'backup_stale', params: { days: '4' }, message: 'x' }, t, now, 'en'),
    'health.reasons.backup_stale {"days":"4"}');
  assert.equal(reasonText({ reason: 'provider_quota', params: { until: new Date(now.getTime() + 27 * day).toISOString() }, message: 'x' }, t, now, 'en'),
    'health.reasons.provider_quota {"until":"in 27 days"}');
  assert.equal(reasonText({ reason: 'new_reason', params: {}, message: 'Its own words' }, t, now, 'en'), 'Its own words');
});

test('"since" is said in the UI language, in the largest unit that fits', () => {
  assert.equal(sinceText(ago(3 * day), now, 'en'), '3 days ago');
  assert.equal(sinceText(ago(5 * 3600 * 1000), now, 'en'), '5 hours ago');
  assert.equal(sinceText(ago(20 * 60 * 1000), now, 'en'), '20 minutes ago');
  assert.equal(sinceText(ago(3 * day), now, 'es'), 'hace 3 días');
});

test('the health texts exist in English and Spanish', () => {
  const reasons = ['provider_quota', 'provider_rate_limited', 'provider_unavailable', 'provider_settings', 'source_signed_out',
    'source_sync_failed', 'source_keepalive_failed', 'source_list_failed', 'backup_failed', 'backup_stale', 'disk_low', 'disk_critical', 'connector_paused'];
  for (const lang of ['en', 'es']) {
    const s = JSON.parse(readFileSync(new URL(`../src/i18n/locales/${lang}.json`, import.meta.url))).health;
    for (const key of ['title', 'intro', 'none', 'unavailable', 'error', 'warning', 'open', 'dot']) {
      assert.ok(s[key], `${lang}: health.${key}`);
    }
    for (const r of reasons) assert.ok(s.reasons[r], `${lang}: health.reasons.${r}`);
    for (const k of ['backups', 'disk']) assert.ok(s.kinds[k], `${lang}: health.kinds.${k}`);
  }
});
