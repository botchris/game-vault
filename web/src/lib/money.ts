/** Number of decimals of a currency, as the browser knows it (EUR 2, JPY 0, BHD 3). */
export function currencyDigits(currency: string): number {
  try {
    return new Intl.NumberFormat('en', { style: 'currency', currency }).resolvedOptions().maximumFractionDigits ?? 2;
  } catch {
    return 2;
  }
}

/**
 * Reads an amount typed by a person into minor units, or null when it is not one. The last "." or
 * "," followed by at most `digits` digits is the decimal separator; any other "." "," or space is a
 * thousands separator: "1.234,50", "1,234.50" and "1234,5" all mean 1234.50.
 */
export function parseAmount(text: string, digits: number): bigint | null {
  const s = text.trim().replace(/\s/g, '');
  if (!/^\d[\d.,]*$/.test(s)) return null;
  const last = Math.max(s.lastIndexOf('.'), s.lastIndexOf(','));
  let whole = s;
  let frac = '';
  if (last >= 0 && s.length - last - 1 <= digits && s.length - last - 1 > 0) {
    whole = s.slice(0, last);
    frac = s.slice(last + 1);
  }
  whole = whole.replace(/[.,]/g, '');
  if (!/^\d+$/.test(whole) || !/^\d*$/.test(frac)) return null;
  return BigInt(whole + frac.padEnd(digits, '0'));
}

/** Formats minor units as money in the UI language: 2995n EUR in Spanish is "29,95 €". */
export function formatAmount(minor: bigint, currency: string, locale: string): string {
  const digits = currencyDigits(currency);
  const value = Number(minor) / 10 ** digits;
  try {
    return new Intl.NumberFormat(locale, { style: 'currency', currency }).format(value);
  } catch {
    return `${value.toFixed(digits)} ${currency}`;
  }
}

/** The amount as the form shows it for editing, in the UI language: 2995n EUR in Spanish is "29,95". */
export function amountInput(minor: bigint, currency: string, locale: string): string {
  if (minor === 0n) return '';
  const digits = currencyDigits(currency);
  return new Intl.NumberFormat(locale, { minimumFractionDigits: digits, maximumFractionDigits: digits, useGrouping: false })
    .format(Number(minor) / 10 ** digits);
}

const REGION_CURRENCY: Record<string, string> = {
  US: 'USD', GB: 'GBP', JP: 'JPY', CA: 'CAD', AU: 'AUD', NZ: 'NZD', CH: 'CHF', MX: 'MXN', BR: 'BRL',
  AR: 'ARS', CL: 'CLP', CO: 'COP', SE: 'SEK', NO: 'NOK', DK: 'DKK', PL: 'PLN', CZ: 'CZK', HU: 'HUF',
  KR: 'KRW', CN: 'CNY', IN: 'INR', ZA: 'ZAR', TR: 'TRY',
};
const EURO = ['AT', 'BE', 'CY', 'DE', 'EE', 'ES', 'FI', 'FR', 'GR', 'HR', 'IE', 'IT', 'LT', 'LU', 'LV', 'MT', 'NL', 'PT', 'SI', 'SK'];

/** The currency of a locale's region ("es-ES" → EUR, "en-US" → USD); EUR when unknown. */
export function regionCurrency(locale: string): string {
  const region = (() => {
    try {
      return new Intl.Locale(locale).maximize().region ?? '';
    } catch {
      return '';
    }
  })();
  if (EURO.includes(region)) return 'EUR';
  return REGION_CURRENCY[region] ?? 'EUR';
}

/** Every currency the browser knows, sorted; a short list on browsers without Intl.supportedValuesOf. */
export function currencyList(): string[] {
  const intl = Intl as unknown as { supportedValuesOf?: (key: string) => string[] };
  return intl.supportedValuesOf?.('currency') ?? ['AUD', 'BRL', 'CAD', 'CHF', 'EUR', 'GBP', 'JPY', 'MXN', 'USD'];
}
