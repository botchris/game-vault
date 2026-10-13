import assert from 'node:assert/strict';
import { test } from 'node:test';
import { isLoopback, isPrivateTransport } from '../lib/origin.js';

// The one table of "this computer" that remembering a confirmation and sending the ownership token
// both rely on.
const loopback = ['localhost', '127.0.0.1', '127.4.5.6', '[::1]'];
const network = ['192.168.1.20', 'gamevault.lan', '10.0.0.5', '[fd00::1]', '127.0.0.1.example', 'localhost.example', '128.0.0.1', ''];

test('this computer is localhost, all of 127.0.0.0/8 and ::1', () => {
  for (const host of loopback) assert.equal(isLoopback(host), true, host);
  for (const host of network) assert.equal(isLoopback(host), false, host);
});

test('an address keeps what it is sent private when it is https or on this computer', () => {
  for (const host of loopback) assert.equal(isPrivateTransport(`http://${host}:8080`), true, host);
  for (const host of network.filter(Boolean)) {
    assert.equal(isPrivateTransport(`http://${host}:8080`), false, host);
    assert.equal(isPrivateTransport(`https://${host}`), true, host);
  }
  for (const origin of ['', 'not an address', undefined, 'ftp://127.0.0.1']) {
    assert.equal(isPrivateTransport(origin), false, String(origin));
  }
});
