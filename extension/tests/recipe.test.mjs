import assert from 'node:assert/strict';
import { test } from 'node:test';
import { validate } from '../lib/recipe.js';
import { cookieHeader, hostsGranted, jsonField, originAllowed, prefixMatch, redirectValue } from '../lib/capture.js';

const humble = () => ({
  version: 1,
  open: 'https://www.humblebundle.com/home/keys',
  when: { urlPrefix: 'https://www.humblebundle.com/home/keys' },
  capture: { cookie: { url: 'https://www.humblebundle.com', name: '_simpleauth_sess' } },
});

test('a sound recipe gives its capture kind, hosts and timeout', () => {
  const ea = { version: 1, open: 'https://www.ea.com/', hosts: ['accounts.ea.com'],
    when: { fetch: { url: 'https://accounts.ea.com/connect/auth?x=1', field: 'access_token' } },
    capture: { cookies: { url: 'https://accounts.ea.com/connect/auth?x=1' } }, timeoutSeconds: 120 };
  assert.deepEqual(validate(humble()), { kind: 'cookie', hosts: ['www.humblebundle.com'], timeoutMs: 300000 });
  assert.deepEqual(validate(ea), { kind: 'cookies', hosts: ['www.ea.com', 'accounts.ea.com'], timeoutMs: 120000 });
});

test('broken or hostile recipes are refused, newer ones as unsupported', () => {
  const cases = {
    invalid: [
      (r) => { r.open = 'http://www.humblebundle.com/'; },
      (r) => { r.capture = {}; },
      (r) => { r.capture.fetch = { url: 'https://www.humblebundle.com/x', field: 'a' }; },
      (r) => { r.capture.cookie.url = 'https://evil.example/'; },
      (r) => { r.when.urlPrefix = 'https://evil.example/'; },
      (r) => { r.hosts = ['https://x.com/']; },
      (r) => { r.timeoutSeconds = 601; },
      (r) => { r.capture.cookie.name = ''; },
      // A condition on a redirect would never be checked: refused rather than silently ignored.
      (r) => { r.capture = { redirect: { prefix: 'https://www.humblebundle.com/done', param: 'code' } }; },
      // A private window's session is not the one an extension fetch sends.
      (r) => { r.private = true; r.when = { fetch: { url: 'https://www.humblebundle.com/api', field: 'a' } }; },
      (r) => { r.private = true; delete r.when; r.capture = { fetch: { url: 'https://www.humblebundle.com/api', field: 'a' } }; },
    ],
    unsupported: [(r) => { r.version = 2; }],
  };
  for (const [code, changes] of Object.entries(cases)) {
    for (const change of changes) {
      const r = humble();
      change(r);
      assert.throws(() => validate(r), (e) => e.code === code, String(change));
    }
  }
  assert.throws(() => validate(null), (e) => e.code === 'invalid');
});

test('capture helpers', () => {
  assert.equal(redirectValue('https://embed.gog.com/on_login_success?origin=client&code=ab%2Fc', 'https://embed.gog.com/on_login_success', 'code'), 'ab/c');
  assert.equal(redirectValue('https://embed.gog.com/other?code=x', 'https://embed.gog.com/on_login_success', 'code'), '');
  assert.equal(cookieHeader([{ name: 'a', value: '1' }, { name: 'b', value: '2' }]), 'a=1; b=2');
  assert.equal(jsonField('{"authorizationCode":"abc","x":1}', 'authorizationCode'), 'abc');
  assert.equal(jsonField('{"authorizationCode":null}', 'authorizationCode'), '');
  assert.equal(jsonField('access_token=tok&expires=1', 'access_token'), 'tok');
  assert.equal(jsonField('<html>', 'npsso'), '');
  assert.equal(originAllowed('http://192.168.1.10:8080', ['http://192.168.1.10:8080']), true);
  assert.equal(originAllowed('http://192.168.1.10:8081', ['http://192.168.1.10:8080']), false);
  assert.equal(originAllowed('https://evil.example', []), false);
  assert.equal(hostsGranted(['a.com', 'b.com'], ['b.com', 'a.com']), true);
  assert.equal(hostsGranted(['a.com', 'c.com'], ['a.com']), false);
});

test('an address matches a prefix only up to a path, query or fragment boundary', () => {
  assert.equal(prefixMatch('https://connect.ubisoft.com/ready', 'https://connect.ubisoft.com/ready'), true);
  assert.equal(prefixMatch('https://connect.ubisoft.com/ready?x=1', 'https://connect.ubisoft.com/ready'), true);
  assert.equal(prefixMatch('https://connect.ubisoft.com/ready/', 'https://connect.ubisoft.com/ready'), true);
  assert.equal(prefixMatch('https://connect.ubisoft.com/ready#a', 'https://connect.ubisoft.com/ready'), true);
  assert.equal(prefixMatch('https://connect.ubisoft.com/readyX', 'https://connect.ubisoft.com/ready'), false);
  assert.equal(prefixMatch('https://www.fanatical.com.evil.example/', 'https://www.fanatical.com'), false);
  assert.equal(prefixMatch('https://www.fanatical.com/en/', 'https://www.fanatical.com'), true);
  assert.equal(prefixMatch('https://www.humblebundle.com/home/keys', 'https://www.humblebundle.com/home/'), true);
  assert.equal(prefixMatch(undefined, 'https://x.com'), false);
  assert.equal(redirectValue('https://embed.gog.com/on_login_successX?code=x', 'https://embed.gog.com/on_login_success', 'code'), '');
});
