import { useEffect, useState } from 'react';
import { providerClient } from '../api/client';

let cache: Promise<Map<string, string>> | null = null;

/** The enabled price providers (id → name), fetched once per page load. */
export function usePriceProviders(): Map<string, string> {
  const [providers, setProviders] = useState<Map<string, string>>(new Map());
  useEffect(() => {
    cache ??= providerClient.listProviders({ kind: 'valuation' }).then(
      (res) => new Map(res.providers.filter((p) => p.enabled).map((p) => [p.id, p.name])),
      () => new Map(),
    );
    let live = true;
    cache.then((m) => { if (live) setProviders(m); });
    return () => { live = false; };
  }, []);
  return providers;
}

/** Forgets the cached list, after the user changed the providers. */
export function forgetPriceProviders() {
  cache = null;
}
