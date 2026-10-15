import assert from 'node:assert/strict';
import { test } from 'node:test';
import { homeSync } from '../src/lib/homeSync.ts';

/** A promise settled from the outside, so a test decides the order answers arrive in. */
function deferred() {
  let resolve, reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

const tick = () => new Promise((r) => setImmediate(r));

/** The server as the page sees it: two blocks, each with its own version, and reads that answer
 *  when the test says so, with the state of the moment they were sent. */
function server() {
  const state = { deals: 'deals v1', releases: 'releases v1' };
  const reads = [];
  return {
    state,
    reads,
    read: () => {
      const d = deferred();
      reads.push({ ...d, seen: { ...state } });
      return d.promise;
    },
    answer: (i) => reads[i].resolve(reads[i].seen),
  };
}

test('a refresh of one block never drops the other block\'s newer answer', async () => {
  const srv = server();
  const shown = [];
  const sync = homeSync(srv.read, (h) => shown.push(h), () => {});

  const deals = deferred();
  const releases = deferred();
  const dealsDone = sync.refresh(() => deals.promise);
  const releasesDone = sync.refresh(() => releases.promise);

  // Releases settle first, deals a moment later; each settled refresh reads the home again.
  srv.state.releases = 'releases v2';
  releases.resolve();
  await tick();
  srv.state.deals = 'deals v2';
  deals.resolve();
  await tick();
  assert.equal(srv.reads.length, 2);

  // The reads answer out of order: the older one, arriving last, must not replace the newer.
  srv.answer(1);
  srv.answer(0);
  await Promise.all([dealsDone, releasesDone]);

  assert.deepEqual(shown, [{ deals: 'deals v2', releases: 'releases v2' }]);
});

test('a failed refresh still reads the home again, and hands back its error and that read', async () => {
  const srv = server();
  const shown = [];
  const sync = homeSync(srv.read, (h) => shown.push(h), () => {});

  const refused = new Error('too soon');
  const done = sync.refresh(() => Promise.reject(refused));
  await tick();
  srv.answer(0);
  const outcome = await done;

  assert.equal(outcome.failed, true);
  assert.equal(outcome.error, refused);
  assert.deepEqual(outcome.fresh, { deals: 'deals v1', releases: 'releases v1' });
  assert.deepEqual(shown, [outcome.fresh]);
});

test('a read that fails reports its error only when no newer read took over', async () => {
  const errors = [];
  const answers = [deferred(), deferred()];
  let n = 0;
  const sync = homeSync(() => answers[n++].promise, () => {}, (e) => errors.push(e));

  const first = sync.load();
  const second = sync.load();
  answers[0].reject(new Error('old'));
  answers[1].reject(new Error('new'));

  assert.equal(await first, null);
  assert.equal(await second, null);
  assert.deepEqual(errors.map((e) => e.message), ['new']);
});
