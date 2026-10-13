import { useAppData } from '../state/AppData';
import { regionCurrency } from './money';

/** The default currency: the saved one (loaded once, in the app's data), else the browser region's. */
export function usePreferredCurrency(locale: string): string {
  return useAppData().preferences.currency || regionCurrency(locale);
}
