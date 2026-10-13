import { useCallback, useEffect, useRef, useState } from 'react';
import { createEscapeStack } from '../lib/escapeStack';

// One stack and one window listener for the whole app, so Escape closes only the overlay on top.
// Overlays are stacked in the order their effects run. React runs a child's effects before its
// parent's, so a parent and a child overlay mounted in the same commit would register child first
// and the parent would be on top; no current overlay mounts that way (each opens on its own).
const stack = createEscapeStack();
let listening = false;

function listen() {
  if (listening) return;
  listening = true;
  window.addEventListener('keydown', (e) => {
    // Escape inside a field must not close the overlay around it. What protects fields today is the
    // Lightbox, whose root stops Escape from propagating, so its caption field never reaches this
    // listener. A field inside a Modal that wants its own Escape (cancelling an edit) must stop
    // propagation or call preventDefault; this listener skips an event already prevented.
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
