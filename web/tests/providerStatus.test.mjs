import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { describeStatus, renewsIn, statusFor } from '../src/lib/providerStatus.ts';

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

test('a provider is matched to its service status by any of the service ids', () => {
  const statuses = [{ providerId: 'thegamesdb', providerIds: ['thegamesdb', 'thegamesdb-details'] }];
  assert.equal(statusFor(statuses, 'thegamesdb-details'), statuses[0]);
  assert.equal(statusFor(statuses, 'steam'), undefined);
});

test('every reason is translated in English and Spanish', () => {
  for (const lang of ['en', 'es']) {
    const s = JSON.parse(readFileSync(new URL(`../src/i18n/locales/${lang}.json`, import.meta.url))).providerStatus;
    for (const key of ['title', 'intro', 'quota', 'rateLimited', 'unavailable', 'settings']) {
      assert.ok(s[key], `${lang}: providerStatus.${key}`);
    }
  }
});
