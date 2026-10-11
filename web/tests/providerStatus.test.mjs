import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { describeStatus, renewsIn } from '../src/lib/providerStatus.ts';

const t = (key, opts) => `${key}${opts ? ' ' + JSON.stringify(opts) : ''}`;
const now = new Date('2026-10-11T12:00:00Z');
const later = (ms) => new Date(now.getTime() + ms);
const day = 24 * 3600 * 1000;

test('a renewal is said in the largest unit that fits, in the UI language', () => {
  assert.equal(renewsIn(later(27 * day), now, 'en'), 'in 27 days');
  assert.equal(renewsIn(later(5 * 3600 * 1000), now, 'en'), 'in 5 hours');
  assert.equal(renewsIn(later(20 * 60 * 1000), now, 'en'), 'in 20 minutes');
  assert.equal(renewsIn(later(27 * day), now, 'es'), 'dentro de 27 días');
});

test('each kind of problem has its own translated reason', () => {
  assert.equal(describeStatus({ kind: 'quota', message: 'x' }, later(27 * day), now, t, 'en'),
    'providerStatus.quota {"when":"in 27 days"}');
  assert.equal(describeStatus({ kind: 'rate_limited', message: 'x' }, undefined, now, t, 'en'), 'providerStatus.rateLimited');
  assert.equal(describeStatus({ kind: 'unavailable', message: 'x' }, undefined, now, t, 'en'), 'providerStatus.unavailable');
  assert.equal(describeStatus({ kind: 'settings', message: 'x' }, undefined, now, t, 'en'), 'providerStatus.settings');
  assert.equal(describeStatus({ kind: 'new_kind', message: 'Its own words' }, undefined, now, t, 'en'), 'Its own words');
});

test('every reason is translated in English and Spanish', () => {
  for (const lang of ['en', 'es']) {
    const s = JSON.parse(readFileSync(new URL(`../src/i18n/locales/${lang}.json`, import.meta.url))).providerStatus;
    for (const key of ['quota', 'rateLimited', 'unavailable', 'settings']) {
      assert.ok(s[key], `${lang}: providerStatus.${key}`);
    }
  }
});
