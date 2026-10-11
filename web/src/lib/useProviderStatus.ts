import { useCallback, useEffect, useState } from 'react';
import { providerClient } from '../api/client';
import type { ProviderStatus } from '../gen/gamevault/v1/provider_pb';

/** How often the statuses are fetched again while a page shows them. */
const REFRESH_MS = 3 * 60 * 1000;

/**
 * The providers' current problems, fetched again when the window regains focus and every few
 * minutes while the page is open: a used-up allowance renews, an outage ends. Best effort: on error
 * the list is empty (the pages work without it).
 */
export function useProviderStatus(): [ProviderStatus[], () => void] {
  const [statuses, setStatuses] = useState<ProviderStatus[]>([]);

  const reload = useCallback(() => {
    providerClient.getProviderStatus({}).then((res) => setStatuses(res.statuses), () => setStatuses([]));
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

  return [statuses, reload];
}
