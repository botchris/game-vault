import { useCallback, useEffect, useRef, useState } from 'react';
import { errorMessage } from '../api/client';

/** How an action reports. */
export interface ActionOptions {
  /** Stay busy after a success: the dialog closes (or the page changes) on success, so its buttons
   *  must not come back to life for the moment before it goes. */
  keepBusyOnSuccess?: boolean;
  /** Report a failure here instead of keeping it in `error` (a notice shown by a parent). */
  onError?: (message: string) => void;
}

/** A user action's state: busy while it runs, its error message once it fails. */
export interface Action {
  busy: boolean;
  /** The last failure's message ('' after a success, or while running). */
  error: string;
  setError: (message: string) => void;
  /** Runs `fn`: busy meanwhile, the error cleared first and set when it throws. Resolves to whether
   *  it succeeded; it never rejects. */
  run: (fn: () => Promise<void>) => Promise<boolean>;
}

/** The busy/error/try-finally of a button that calls the server, written once. */
export function useAction(options: ActionOptions = {}): Action {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // The latest options, so `run` keeps one identity while a parent passes a new onError each render.
  const latest = useRef(options);
  useEffect(() => {
    latest.current = options;
  });
  const run = useCallback(async (fn: () => Promise<void>) => {
    setBusy(true);
    setError('');
    try {
      await fn();
      if (!latest.current.keepBusyOnSuccess) setBusy(false);
      return true;
    } catch (e) {
      const message = errorMessage(e);
      if (latest.current.onError) latest.current.onError(message);
      else setError(message);
      setBusy(false);
      return false;
    }
  }, []);
  return { busy, error, setError, run };
}
