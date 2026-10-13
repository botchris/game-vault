// Which overlay Escape belongs to. Overlays open on top of each other (a search dialog inside the
// game sheet, the image viewer over a dialog), and if each listened for Escape on its own, one key
// press would close all of them at once. They register here instead, in the order they open, and
// only the one opened last is told about Escape.

/** An overlay's Escape handler. */
export type EscapeHandler = () => void;

/** The overlays open right now, in the order they opened. */
export interface EscapeStack {
  /** Registers an overlay on top of the others; the returned function removes it (safe to call twice). */
  push: (handler: EscapeHandler) => () => void;
  /** Runs the top overlay's handler; false when no overlay is open. */
  handle: () => boolean;
  /** Whether `handler` belongs to the overlay on top. */
  isTop: (handler: EscapeHandler) => boolean;
  /** How many overlays are open. */
  size: () => number;
}

/** A new, empty stack. The app uses the shared one in `components/useEscape`. */
export function createEscapeStack(): EscapeStack {
  // Entries are wrapped so the same function can be pushed twice and still popped one at a time.
  const entries: { handler: EscapeHandler }[] = [];
  return {
    push: (handler) => {
      const entry = { handler };
      entries.push(entry);
      return () => {
        const at = entries.indexOf(entry);
        if (at >= 0) entries.splice(at, 1);
      };
    },
    handle: () => {
      const top = entries[entries.length - 1];
      if (!top) return false;
      top.handler();
      return true;
    },
    isTop: (handler) => entries[entries.length - 1]?.handler === handler,
    size: () => entries.length,
  };
}
