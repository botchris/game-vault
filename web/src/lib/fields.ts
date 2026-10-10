// Custom fields in the web: formatting, number input, search and filters. Structural types only,
// so Node tests run it as is (no generated code, no TypeScript enums).

/** One option of a list field. */
export interface Choice { id: string; name: string }

/** A field definition, shaped like the generated message. */
export interface Definition {
  id: string; name: string; type: string; scope: string; kinds: number[];
  decimals: number; unit: string; currency: string; choices: Choice[];
}

/** An amount in minor units with its currency. */
export interface Money { amountMinor: bigint; currency: string }

/** A field value, shaped like the generated message. */
export type Value = { value:
  | { case: 'text'; value: string } | { case: 'bool'; value: boolean } | { case: 'number'; value: bigint }
  | { case: 'money'; value: Money } | { case: 'date'; value: string } | { case: 'minutes'; value: bigint }
  | { case: 'choice'; value: string } | { case: 'choices'; value: { ids: string[] } } | { case: undefined; value?: undefined } };

/** Values by field id. */
export type Values = Record<string, Value>;
interface CopyLike { details?: { kind: number; fields: Values } }
interface GameLike { fields: Values; copies: CopyLike[] }

/** Locale-dependent pieces the caller supplies to format values. */
export interface Formatters {
  /** The UI language, for numbers (BCP 47 tag, e.g. "es"). */
  locale: string;
  money: (amountMinor: bigint, currency: string) => string;
  date: (d: string) => string;
  yes: string;
  no: string;
  hours: string;
  minutes: string;
}

/** Filter values: choice ids, 'yes' / 'no', or '' for "no value". */
export type FieldFilter = Record<string, string[]>;

/** The decimal separator of a locale ("." in en, "," in es). */
export function decimalSeparator(locale?: string): string {
  return new Intl.NumberFormat(locale).formatToParts(1.5).find((p) => p.type === 'decimal')?.value ?? '.';
}

/**
 * A number as the text of an input: hundredths become a decimal when the field has 2 decimals,
 * written with the locale's decimal separator (no grouping, so the text parses back as typed).
 */
export function numberInput(v: bigint, decimals: number, locale?: string): string {
  if (decimals !== 2) return v.toString();
  const neg = v < 0n;
  const abs = neg ? -v : v;
  return `${neg ? '-' : ''}${abs / 100n}${decimalSeparator(locale)}${(abs % 100n).toString().padStart(2, '0')}`;
}

/**
 * A number for reading, in the locale's format ("1,234.50" in en, "1234,50" in es). The whole part
 * is formatted as a bigint and the hundredths are appended, so no value goes through a float.
 */
export function formatNumber(v: bigint, decimals: number, locale?: string): string {
  const neg = v < 0n;
  const abs = neg ? -v : v;
  const whole = decimals === 2 ? abs / 100n : abs;
  let text = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(whole);
  if (decimals === 2) text += decimalSeparator(locale) + (abs % 100n).toString().padStart(2, '0');
  return neg ? `-${text}` : text;
}

/**
 * The value typed in a number input, in hundredths when the field has 2 decimals; null when invalid
 * or empty. A dot or a comma is the decimal separator, whatever the locale, and either side of it may
 * be missing (".5", "12."); grouping separators are not accepted, since "1,234" would be ambiguous.
 */
export function parseNumber(input: string, decimals: number): bigint | null {
  const s = input.trim().replace(',', '.');
  const m = decimals === 2 ? /^(-?)(\d*)(?:\.(\d{0,2}))?$/.exec(s) : /^(-?)(\d+)\.?$/.exec(s);
  if (!m || (m[2] === '' && !m[3])) return null;
  const whole = BigInt(m[2] || '0');
  const cents = decimals === 2 ? BigInt((m[3] ?? '').padEnd(2, '0')) : 0n;
  const v = decimals === 2 ? whole * 100n + cents : whole;
  return m[1] ? -v : v;
}

/**
 * Whether a value has something to show: not empty, not blank text, and for lists at least one
 * choice the field still has (a value whose choice was removed shows nothing).
 */
export function hasDisplayValue(def: Definition, v: Value | undefined): boolean {
  const val = v?.value;
  if (!val || val.case === undefined) return false;
  if (val.case === 'text') return val.value.trim() !== '';
  if (val.case === 'choice') return def.choices.some((c) => c.id === val.value);
  if (val.case === 'choices') return def.choices.some((c) => val.value.ids.includes(c.id));
  return true;
}

/** The choices a multi-value list holds, in the field's order, without removed ones. */
export function chosen(def: Definition, v: Value | undefined): Choice[] {
  const val = v?.value;
  if (val?.case === 'choices') return def.choices.filter((c) => val.value.ids.includes(c.id));
  if (val?.case === 'choice') return def.choices.filter((c) => c.id === val.value);
  return [];
}

/** A value as text for the sheet and the copy card. */
export function fieldText(def: Definition, v: Value | undefined, fmt: Formatters): string {
  const val = v?.value;
  if (!val || val.case === undefined || !hasDisplayValue(def, v)) return '';
  switch (val.case) {
    case 'text': return val.value;
    case 'bool': return val.value ? fmt.yes : fmt.no;
    case 'number': return [formatNumber(val.value, def.decimals, fmt.locale), def.unit].filter(Boolean).join(' ');
    case 'money': return fmt.money(val.value.amountMinor, val.value.currency);
    case 'date': return fmt.date(val.value);
    case 'minutes': {
      const h = val.value / 60n; const m = val.value % 60n;
      const n = (x: bigint) => formatNumber(x, 0, fmt.locale);
      return [h > 0n && `${n(h)} ${fmt.hours}`, m > 0n && `${n(m)} ${fmt.minutes}`].filter(Boolean).join(' ') || `${n(0n)} ${fmt.minutes}`;
    }
    case 'choice': return def.choices.find((c) => c.id === val.value)?.name ?? '';
    case 'choices': return chosen(def, v).map((c) => c.name).join(', ');
  }
}

const textOf = (v?: Value) => (v?.value.case === 'text' ? v.value.value : '');

/** The text of a game's (and its copies') text and long-text fields, for the search box. */
export function searchableText(g: GameLike, defs: Definition[]): string {
  const texts = defs.filter((d) => d.type === 'text' || d.type === 'longtext');
  return texts.flatMap((d) => (d.scope === 'game' ? [textOf(g.fields[d.id])] : g.copies.map((c) => textOf(c.details?.fields[d.id]))))
    .filter(Boolean).join(' ');
}

/** Whether a field can be a library filter: only types with a fixed set of values. */
export function isFilterable(def: Definition): boolean {
  return def.type === 'list' || def.type === 'multilist' || def.type === 'bool';
}

/**
 * The filter keys a value matches: choice ids, 'yes' / 'no', or '' when it has none. A value of a
 * type that is not filterable (text, number…) matches no key.
 */
export function keysOf(v: Value | undefined): string[] {
  const val = v?.value;
  if (!val || val.case === undefined) return [''];
  if (val.case === 'choice') return [val.value];
  if (val.case === 'choices') return val.value.ids.length ? val.value.ids : [''];
  if (val.case === 'bool') return [val.value ? 'yes' : 'no'];
  return [];
}

function gameKeys(g: GameLike, def: Definition): string[] {
  if (def.scope === 'game') return keysOf(g.fields[def.id]);
  const copies = g.copies.filter((c) => c.details && (!def.kinds.length || def.kinds.includes(c.details.kind)));
  return copies.length ? copies.flatMap((c) => keysOf(c.details!.fields[def.id])) : [''];
}

/** A game matches when, for every filtered field, one of its keys is among the chosen ones. */
export function matchesFieldFilters(g: GameLike, defs: Definition[], filter: FieldFilter): boolean {
  return defs.every((d) => {
    const chosen = filter[d.id];
    if (!chosen?.length) return true;
    return gameKeys(g, d).some((k) => chosen.includes(k));
  });
}

/** How many games each filter option matches, in display order ('' last). */
export function filterOptions(games: GameLike[], def: Definition): { key: string; count: number }[] {
  if (!isFilterable(def)) return [];
  const keys = def.type === 'bool' ? ['yes', 'no'] : def.choices.map((c) => c.id);
  return [...keys, ''].map((key) => ({ key, count: games.filter((g) => gameKeys(g, def).includes(key)).length }));
}

/**
 * The filter without what no longer exists: fields deleted or no longer filterable, and choices
 * removed from their list. Stale ids would count in the filter badge and could not be cleared,
 * since the panel shows no option for them. Returns the same object when nothing was dropped.
 */
export function pruneFieldFilter(filter: FieldFilter, defs: Definition[]): FieldFilter {
  const out: FieldFilter = {};
  let changed = false;
  for (const [id, keys] of Object.entries(filter)) {
    const def = defs.find((d) => d.id === id);
    const valid = def && isFilterable(def)
      ? new Set(['', ...(def.type === 'bool' ? ['yes', 'no'] : def.choices.map((c) => c.id))])
      : new Set<string>();
    const kept = keys.filter((k) => valid.has(k));
    if (kept.length !== keys.length) changed = true;
    if (kept.length) out[id] = kept;
  }
  return changed ? out : filter;
}

/**
 * The values a copy of the given kind keeps when saved: those of the copy fields that apply to the
 * kind, and those of fields the page does not know (definitions not loaded, or made in another tab),
 * which are kept as they are rather than erased, since the server replaces a copy's values.
 */
export function copyValuesFor<V>(values: Record<string, V>, defs: Definition[], kind: number): Record<string, V> {
  const known = new Set(defs.map((d) => d.id));
  const applies = new Set(defs.filter((d) => d.scope === 'copy' && (!d.kinds.length || d.kinds.includes(kind))).map((d) => d.id));
  return Object.fromEntries(Object.entries(values).filter(([id]) => applies.has(id) || !known.has(id)));
}
