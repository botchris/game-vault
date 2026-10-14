import assert from 'node:assert/strict';
import { test } from 'node:test';
import { onBackdropPress } from '../src/lib/backdrop.ts';

// A press as React hands it to the backdrop's handler: where it started and the backdrop itself.
function press(target, currentTarget) {
  const event = { target, currentTarget, stopped: false, stopPropagation() { this.stopped = true; } };
  return event;
}

test('a press on a dialog\'s backdrop closes that dialog and goes no further', () => {
  let closed = 0;
  const backdrop = {};
  const event = press(backdrop, backdrop);
  onBackdropPress(() => { closed++; })(event);
  assert.equal(closed, 1);
  assert.equal(event.stopped, true, 'the overlay under it (the game sheet) must not see it');
});

test('a press that started inside the dialog does not close it, and stops there too', () => {
  let closed = 0;
  const backdrop = {}, button = {};
  const event = press(button, backdrop);
  onBackdropPress(() => { closed++; })(event);
  assert.equal(closed, 0);
  assert.equal(event.stopped, true);
});
