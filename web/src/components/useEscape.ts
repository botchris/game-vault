import { useCallback, useEffect, useRef, useState } from 'react';
import { createEscapeStack } from '../lib/escapeStack';

// One stack and one window listener for the whole app, so Escape closes only the overlay on top.
const stack = createEscapeStack();
let listening = false;

function listen() {
  if (listening) return;
  listening = true;
  window.addEventListener('keydown', (e) => {
    // A field that handles Escape itself (cancelling an edit) marks the event as used.
    if (e.key !== 'Escape' || e.defaultPrevented) return;
    if (stack.handle()) e.preventDefault();
  });
}

/** Calls `onEscape` when Escape is pressed while this overlay is the one on top (the last one
 *  opened). Returns a function telling whether it is on top, for overlays that also handle other
 *  keys (the sheet's ← →) and must leave them alone while something covers them. */
export function useEscape(onEscape: () => void): () => boolean {
  const latest = useRef(onEscape);
  useEffect(() => {
    latest.current = onEscape;
  });
  // The registered function never changes, so a new `onEscape` on a later render does not move
  // this overlay above one opened after it.
  const [handler] = useState(() => () => latest.current());
  useEffect(() => {
    listen();
    return stack.push(handler);
  }, [handler]);
  return useCallback(() => stack.isTop(handler), [handler]);
}
