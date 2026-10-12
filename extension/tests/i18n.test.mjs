import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const load = (path) => JSON.parse(readFileSync(new URL(path, import.meta.url), 'utf8'));

test('every extension text exists in English and Spanish, with the same placeholders', () => {
  const en = load('../_locales/en/messages.json');
  const es = load('../_locales/es/messages.json');
  assert.deepEqual(Object.keys(es).sort(), Object.keys(en).sort());
  for (const key of Object.keys(en)) {
    assert.deepEqual(Object.keys(es[key].placeholders ?? {}).sort(), Object.keys(en[key].placeholders ?? {}).sort(), key);
  }
});

test('the manifest falls back to English', () => {
  assert.equal(load('../manifest.json').default_locale, 'en');
});
