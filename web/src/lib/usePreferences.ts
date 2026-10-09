import { useEffect, useState } from 'react';
import { systemClient } from '../api/client';
import { regionCurrency } from './money';

/** The default currency: the saved one, else the browser region's. */
export function usePreferredCurrency(locale: string): string {
  const [currency, setCurrency] = useState(() => regionCurrency(locale));
  useEffect(() => {
    systemClient.getPreferences({}).then((r) => { if (r.preferences?.currency) setCurrency(r.preferences.currency); }, () => {});
  }, []);
  return currency;
}
