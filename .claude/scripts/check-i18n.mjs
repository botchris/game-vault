#!/usr/bin/env node
// Checks that every UI language has the same translation keys as English.
// Plural forms (key_one / key_other) are compared as written. Exit code 1 lists the differences.
// Run it with `task i18n` (inside the toolchain container).
import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const locales = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'web', 'src', 'i18n', 'locales');

function keys(node, prefix = '') {
  return Object.entries(node).flatMap(([k, v]) =>
    v && typeof v === 'object' ? keys(v, `${prefix}${k}.`) : [`${prefix}${k}`]);
}

const load = (file) => new Set(keys(JSON.parse(readFileSync(join(locales, file), 'utf8'))));
const reference = load('en.json');
let ok = true;
for (const file of readdirSync(locales).filter((f) => f.endsWith('.json') && f !== 'en.json').sort()) {
  const other = load(file);
  for (const k of [...reference].filter((k) => !other.has(k)).sort()) { ok = false; console.log(`${file}: missing ${k}`); }
  for (const k of [...other].filter((k) => !reference.has(k)).sort()) { ok = false; console.log(`${file}: extra ${k}`); }
}
console.log(ok ? 'translations OK' : 'translations differ');
process.exit(ok ? 0 : 1);
