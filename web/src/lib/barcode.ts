// Barcode helpers with no other dependency, so Node tests can import them.

/** Whether code is an EAN-13, UPC-A or EAN-8 with a valid check digit (spaces and dashes ignored). */
export function validBarcode(code: string): boolean {
  const d = code.replace(/[\s-]/g, '');
  if (!/^(\d{8}|\d{12}|\d{13})$/.test(d)) return false;
  let sum = 0;
  for (let i = d.length - 2, w = 3; i >= 0; i--, w = w === 3 ? 1 : 3) sum += Number(d[i]) * w;
  return (10 - (sum % 10)) % 10 === Number(d[d.length - 1]);
}

/** The code's digits, a UPC-A written as its EAN-13 (a leading 0), as the server stores it. */
export function normalizeBarcode(code: string): string {
  const d = code.replace(/\D/g, '');
  return d.length === 12 ? `0${d}` : d;
}

/**
 * Pages where the user can look a code up by hand when no barcode database knows it: EAN-Search's
 * own search and a web search for the exact code. They open in the user's browser; Game Vault
 * never queries or reads them (EAN-Search's terms forbid automated use of its site).
 */
export function lookupLinks(code: string): { eanSearch: string; web: string } {
  const c = normalizeBarcode(code);
  return {
    eanSearch: `https://www.ean-search.org/?q=${c}`,
    web: `https://www.google.com/search?q=${encodeURIComponent(`"${c}"`)}`,
  };
}
