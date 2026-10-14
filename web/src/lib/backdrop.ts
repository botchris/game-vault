// Closing an overlay by pressing its backdrop. Dialogs open over other overlays (a dialog over the
// game sheet) and are rendered inside them, so a press on a dialog's backdrop would bubble on to
// the backdrop under it and close that one too (or start its "unsaved changes" question). The
// press belongs to the overlay on top: it closes that one and goes no further.

/** The part of a mouse event the backdrop handler needs. */
export interface BackdropEvent {
  target: unknown;
  currentTarget: unknown;
  stopPropagation: () => void;
}

/** A backdrop's mousedown handler: a press on the backdrop itself calls `close`; no press, on it or
 *  inside what it holds, reaches the overlays under it. */
export function onBackdropPress(close: () => void): (e: BackdropEvent) => void {
  return (e) => {
    e.stopPropagation();
    if (e.target === e.currentTarget) close();
  };
}
