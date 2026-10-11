/**
 * Returns the address when it is an absolute https URL, undefined otherwise. Addresses that come
 * from providers (an article's page, a license) only become links when they are https, so a
 * `javascript:` or plain-http value can never end up in an href.
 */
export function httpsUrl(value: string | undefined): string | undefined {
  if (!value) return undefined;
  try {
    return new URL(value).protocol === 'https:' ? value : undefined;
  } catch {
    return undefined;
  }
}

/** Whether the link on store `key` is still the one Game Vault found automatically: the server
 *  marks it, and the user has not changed it in the editor (`edited`), so it gets the "automatic"
 *  badge. */
export function isAutoLink(game: { links: Record<string, string>; autoLinks: string[] }, key: string, edited: string | undefined): boolean {
  return game.autoLinks.includes(key) && (edited ?? '').trim() === game.links[key];
}

/**
 * Rebases a form's edits on the record as it is now: the keys the user changed (`edited` differs
 * from what the form `loaded`, a key removed included) keep the user's value, and every other key
 * comes from `current`. A link found automatically while the form was open, or one added from
 * another page, is kept instead of being silently removed by a save. `same` compares two values
 * (by default strictly).
 */
export function rebaseRecord<V>(
  loaded: Record<string, V>,
  edited: Record<string, V>,
  current: Record<string, V>,
  same: (a: V, b: V) => boolean = (a, b) => a === b,
): Record<string, V> {
  const out = { ...current };
  for (const key of new Set([...Object.keys(loaded), ...Object.keys(edited)])) {
    const was = loaded[key], now = edited[key];
    const changed = was === undefined || now === undefined ? was !== now : !same(was, now);
    if (!changed) continue;
    if (now === undefined) delete out[key];
    else out[key] = now;
  }
  return out;
}
