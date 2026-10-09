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
