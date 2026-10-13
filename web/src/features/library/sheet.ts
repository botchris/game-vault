import type { Copy, Game } from '../../lib/model';

// Types shared by the game sheet (GameDetail) and its parts: the hero, the tabs and the dialogs.

/** Moves between the games of the list a sheet was opened from. */
export interface SheetNav {
  previous?: () => void;
  next?: () => void;
}

/** A dialog opened from the sheet, over it. */
export type SheetDialog =
  | { type: 'addCopy' }
  | { type: 'editCopy'; copy: Copy }
  | { type: 'moveCopy'; copy: Copy }
  | { type: 'merge' }
  | { type: 'coverPicker'; photosOnly?: boolean };

/** The editable fields a change sends; the rest are sent back as they are. */
export type GamePatch = Partial<Pick<Game, 'title' | 'links' | 'notes' | 'playStatus' | 'rating' | 'fields'>>;

/** What the sheet's parts share: one busy flag for the sheet's actions (their errors show at the
 *  top of the sheet's body), and the dialogs it opens. */
export interface SheetActions {
  busy: boolean;
  /** Runs an action of the sheet: busy while it runs, its error shown on the sheet. */
  run: (fn: () => Promise<void>) => Promise<void>;
  openDialog: (d: SheetDialog) => void;
}
