import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { healthClient } from '../api/client';
import type { HealthIssue } from '../gen/gamevault/v1/health_pb';

/** How often the issues are fetched again. */
const REFRESH_MS = 3 * 60 * 1000;

interface HealthState {
  /** The current issues (the last ones known when a fetch fails). */
  issues: HealthIssue[];
  /** The last fetch failed: the list may be stale, and "No issues found" would be untrue. */
  unavailable: boolean;
  /** Fetches the issues now, e.g. after saving a source or a provider. */
  reload: () => void;
}

const HealthContext = createContext<HealthState>({ issues: [], unavailable: false, reload: () => {} });

/**
 * What needs the user's attention (System → Health), fetched once for the whole app: on start, when
 * the window regains focus, every few minutes, and on reload() after a change that may fix an issue.
 */
export function HealthProvider({ children }: { children: ReactNode }) {
  const [issues, setIssues] = useState<HealthIssue[]>([]);
  const [unavailable, setUnavailable] = useState(false);

  const reload = useCallback(() => {
    healthClient.getHealth({}).then(
      (res) => { setIssues(res.issues); setUnavailable(false); },
      () => setUnavailable(true), // keep the last list
    );
  }, []);

  useEffect(() => {
    reload();
    const timer = window.setInterval(reload, REFRESH_MS);
    window.addEventListener('focus', reload);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener('focus', reload);
    };
  }, [reload]);

  const value = useMemo(() => ({ issues, unavailable, reload }), [issues, unavailable, reload]);
  return <HealthContext.Provider value={value}>{children}</HealthContext.Provider>;
}

/** The app's health issues (see HealthProvider). */
export function useHealth(): HealthState {
  return useContext(HealthContext);
}
