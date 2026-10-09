/**
 * ISO 4217 currencies whose minor unit is not two digits. The same table as the server
 * (internal/domain/game/physical.go): amounts are stored in minor units, so the UI and the server
 * must agree on the decimals. The browser's own data (CLDR) differs for some codes (IQD, ESP…).
 */
const CURRENCY_DIGITS: Record<string, number> = {
  BIF: 0, CLP: 0, DJF: 0, GNF: 0, ISK: 0, JPY: 0, KMF: 0, KRW: 0, PYG: 0,
  RWF: 0, UGX: 0, UYI: 0, VND: 0, VUV: 0, XAF: 0, XOF: 0, XPF: 0,
  BHD: 3, IQD: 3, JOD: 3, KWD: 3, LYD: 3, OMR: 3, TND: 3,
};

/** Number of decimals of a currency (EUR 2, JPY 0, BHD 3), as the server counts them. */
export function currencyDigits(currency: string): number {
  return CURRENCY_DIGITS[currency.toUpperCase()] ?? 2;
}

/** A run of digits split by one grouping separator: "1.234.567" → "1234567"; null when the groups
 * are not 1–3 digits then exactly 3 each. */
function ungroup(s: string): string | null {
  if (/^\d+$/.test(s)) return s;
  const sep = s.match(/[.,]/)![0];
  const groups = s.split(sep);
  if (/[.,]/.test(groups.join('')) || !/^\d{1,3}$/.test(groups[0]) || groups.slice(1).some((g) => !/^\d{3}$/.test(g))) return null;
  return groups.join('');
}

/**
 * Reads an amount typed by a person into minor units, or null when it is not one. The last "." or
 * "," followed by 1 to `digits` digits is the decimal separator; before it, the other one may group
 * thousands in threes. "1.234,50", "1,234.50" and "1234,5" mean 1234.50; "1.2.3", or "29,95" for a
 * currency without decimals, are refused rather than guessed.
 */
export function parseAmount(text: string, digits: number): bigint | null {
  const s = text.trim().replace(/\s/g, '');
  if (!/^\d[\d.,]*$/.test(s)) return null;
  const last = Math.max(s.lastIndexOf('.'), s.lastIndexOf(','));
  const after = last >= 0 ? s.length - last - 1 : 0;
  let whole = s;
  let frac = '';
  if (last >= 0 && after >= 1 && after <= digits) {
    whole = s.slice(0, last);
    frac = s.slice(last + 1);
    if (whole.includes(s[last])) return null; // the decimal separator cannot also group
  }
  const plain = ungroup(whole);
  if (plain === null || !/^\d*$/.test(frac)) return null;
  return BigInt(plain + frac.padEnd(digits, '0'));
}

/** Formats minor units as money in the UI language: 2995n EUR in Spanish is "29,95 €". */
export function formatAmount(minor: bigint, currency: string, locale: string): string {
  const digits = currencyDigits(currency);
  const value = Number(minor) / 10 ** digits;
  try {
    return new Intl.NumberFormat(locale, { style: 'currency', currency, minimumFractionDigits: digits, maximumFractionDigits: digits }).format(value);
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
