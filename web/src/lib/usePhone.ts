import { useSyncExternalStore } from 'react';

/** The phone layout's breakpoint, the same as styles.css's. */
export const PHONE = '(max-width: 860px)';

export function subscribePhone(onChange: () => void): () => void {
  const query = window.matchMedia(PHONE);
  query.addEventListener('change', onChange);
  return () => query.removeEventListener('change', onChange);
}

export function isPhone(): boolean {
  return window.matchMedia(PHONE).matches;
}

/** Whether the page has the phone layout, following the window as it is resized. */
export const usePhone = () => useSyncExternalStore(subscribePhone, isPhone);
