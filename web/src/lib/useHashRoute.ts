import { useMemo, useSyncExternalStore } from 'react';
import { parseHash, type Route } from './route';

// The one place that listens to the address. Pages read the route from the hook and change the
// address through replaceRoute (no history entry) or a link / `location.hash` (a new entry); both
// reach every reader, so no page keeps a route of its own that the address no longer says.

const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  window.addEventListener('hashchange', listener);
  return () => {
    listeners.delete(listener);
    window.removeEventListener('hashchange', listener);
  };
}

const currentHash = () => window.location.hash;

/** Replaces the address without a history entry (history.replaceState fires no hashchange, so the
 *  readers are told here). */
export function replaceRoute(hash: string) {
  window.history.replaceState(null, '', hash);
  listeners.forEach((l) => l());
}

/** The route the address names, following it as it changes. */
export function useHashRoute(): Route {
  const hash = useSyncExternalStore(subscribe, currentHash);
  return useMemo(() => parseHash(hash), [hash]);
}
