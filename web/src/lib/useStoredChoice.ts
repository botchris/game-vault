import { useCallback, useState } from 'react';
import { recallChoice, remember } from './storage';

/** A view choice remembered on this device (see storage.ts): one of `allowed`, `fallback` until
 *  the user picks another. The setter is stable, so effects can depend on it. */
export function useStoredChoice<T extends string>(key: string, fallback: T, allowed: readonly T[]): [T, (v: T) => void] {
  const [value, setValue] = useState<T>(() => recallChoice(key, fallback, allowed));
  const set = useCallback((v: T) => {
    setValue(v);
    remember(key, v);
  }, [key]);
  return [value, set];
}
