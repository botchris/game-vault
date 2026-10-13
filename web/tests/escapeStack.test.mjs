import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createEscapeStack } from '../src/lib/escapeStack.ts';

test('Escape reaches only the overlay opened last', () => {
  const stack = createEscapeStack();
  const calls = [];
  stack.push(() => calls.push('sheet'));
  stack.push(() => calls.push('search'));
  assert.equal(stack.handle(), true);
  assert.deepEqual(calls, ['search']);
});

test('closing the top overlay hands Escape back to the one below', () => {
  const stack = createEscapeStack();
  const calls = [];
  stack.push(() => calls.push('sheet'));
  const pop = stack.push(() => calls.push('search'));
  pop();
  stack.handle();
  assert.deepEqual(calls, ['sheet']);
});

test('an overlay below the top can close without disturbing the top one', () => {
  const stack = createEscapeStack();
  const calls = [];
  const popSheet = stack.push(() => calls.push('sheet'));
  stack.push(() => calls.push('search'));
  popSheet();
  stack.handle();
  assert.deepEqual(calls, ['search']);
  assert.equal(stack.size(), 1);
});

test('popping twice removes the entry once', () => {
  const stack = createEscapeStack();
  stack.push(() => {});
  const pop = stack.push(() => {});
  pop();
  pop();
  assert.equal(stack.size(), 1);
});

test('isTop tells an overlay whether it is the one on top', () => {
  const stack = createEscapeStack();
  const sheet = () => {};
  const search = () => {};
  stack.push(sheet);
  assert.equal(stack.isTop(sheet), true);
  const pop = stack.push(search);
  assert.equal(stack.isTop(sheet), false);
  assert.equal(stack.isTop(search), true);
  pop();
  assert.equal(stack.isTop(sheet), true);
});

test('with no overlay open Escape is not handled', () => {
  const stack = createEscapeStack();
  assert.equal(stack.handle(), false);
});
