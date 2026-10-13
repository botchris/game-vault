// View preferences remembered on this device (sort, list or posters, the scan list). localStorage
// can be missing or throw (private mode, blocked or cleared site data), so every access goes
// through here and a failure only means the value is not remembered. Anything that must persist
// goes to the server instead.

/** The value saved under `key`, or null when there is none or storage is unavailable. */
export function recall(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

/** Saves `value` under `key`; false when storage refused it (not remembered). */
export function remember(key: string, value: string): boolean {
  try {
    localStorage.setItem(key, value);
    return true;
  } catch {
    return false;
  }
}

/** Removes what is saved under `key` (nothing happens when storage is unavailable). */
export function forget(key: string): void {
  try {
    localStorage.removeItem(key);
  } catch {
    // Storage blocked: nothing was kept.
  }
}

/** The choice saved under `key` while it is one of `allowed` (an older version may have saved
 *  another), else `fallback`. */
export function recallChoice<T extends string>(key: string, fallback: T, allowed: readonly T[]): T {
  const saved = recall(key) as T | null;
  return saved !== null && allowed.includes(saved) ? saved : fallback;
}
