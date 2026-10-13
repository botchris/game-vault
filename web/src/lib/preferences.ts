// The user's preferences as the UI holds them (state/AppData.tsx) and what System shows of them.
// Plain functions, Node-tested.

/** The preferences once loaded; until then, or when loading failed, unknown with the error. */
export type PreferencesState =
  | { loaded: true; currency: string }
  | { loaded: false; error: string };

/** What System's currency setting shows. `unknown`: the preferences could not be read, so the
 *  region's currency is shown but not offered for saving, which could overwrite a saved one. */
export function currencyView(p: PreferencesState, region: string): { currency: string; saved: boolean; unknown: boolean; error: string } {
  if (!p.loaded) return { currency: region, saved: false, unknown: true, error: p.error };
  return { currency: p.currency || region, saved: p.currency !== '', unknown: false, error: '' };
}
