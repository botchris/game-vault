// Home fades in once, and only when the app opens on it: the shell notes the first page it shows,
// and Home takes the entrance the first time it renders if that page was Home. Coming to Home from
// another tab, or back to it later, never animates (page switches do not move, see ui.md).

let openedOn: string | null = null;
let taken = false;

/** Notes the page the app opened on; only the first call counts, so calling it on every render is fine. */
export function noteOpeningPage(page: string) {
  if (openedOn === null) openedOn = page;
}

/** Whether Home should fade in now: the app opened on it and it has not been shown yet. */
export function homeEntrance(): boolean {
  return openedOn === 'home' && !taken;
}

/** Marks Home's entrance as used, once Home has been on screen. */
export function homeEntranceTaken() {
  taken = true;
}
