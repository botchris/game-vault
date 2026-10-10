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
  money: (amountMinor: bigint, currency: string) => string;
  date: (d: string) => string;
  yes: string;
  no: string;
  hours: string;
  minutes: string;
}

/** Filter values: choice ids, 'yes' / 'no', or '' for "no value". */
export type FieldFilter = Record<string, string[]>;

/** A number as the text of an input: hundredths become a decimal when the field has 2 decimals. */
export function numberInput(v: bigint, decimals: number): string {
  if (decimals !== 2) return v.toString();
  const neg = v < 0n;
  const abs = neg ? -v : v;
  return `${neg ? '-' : ''}${abs / 100n}.${(abs % 100n).toString().padStart(2, '0')}`;
}

/** The value typed in a number input, in hundredths when the field has 2 decimals; null when invalid or empty. */
export function parseNumber(input: string, decimals: number): bigint | null {
  const s = input.trim().replace(',', '.');
  if (!s) return null;
  const m = decimals === 2 ? /^(-?)(\d+)(?:\.(\d{1,2}))?$/.exec(s) : /^(-?)(\d+)$/.exec(s);
  if (!m) return null;
  const whole = BigInt(m[2]!);
  const cents = decimals === 2 ? BigInt((m[3] ?? '').padEnd(2, '0') || '0') : 0n;
  const v = decimals === 2 ? whole * 100n + cents : whole;
  return m[1] ? -v : v;
}

/** A value as text for the sheet and the copy card. */
export function fieldText(def: Definition, v: Value | undefined, fmt: Formatters): string {
  const val = v?.value;
  if (!val || val.case === undefined) return '';
  switch (val.case) {
    case 'text': return val.value;
    case 'bool': return val.value ? fmt.yes : fmt.no;
    case 'number': return [numberInput(val.value, def.decimals), def.unit].filter(Boolean).join(' ');
    case 'money': return fmt.money(val.value.amountMinor, val.value.currency);
    case 'date': return fmt.date(val.value);
    case 'minutes': {
      const h = val.value / 60n; const m = val.value % 60n;
      return [h > 0n && `${h} ${fmt.hours}`, m > 0n && `${m} ${fmt.minutes}`].filter(Boolean).join(' ') || `0 ${fmt.minutes}`;
    }
    case 'choice': return def.choices.find((c) => c.id === val.value)?.name ?? '';
    case 'choices': return def.choices.filter((c) => val.value.ids.includes(c.id)).map((c) => c.name).join(', ');
  }
}

const textOf = (v?: Value) => (v?.value.case === 'text' ? v.value.value : '');

/** The text of a game's (and its copies') text and long-text fields, for the search box. */
export function searchableText(g: GameLike, defs: Definition[]): string {
  const texts = defs.filter((d) => d.type === 'text' || d.type === 'longtext');
  return texts.flatMap((d) => (d.scope === 'game' ? [textOf(g.fields[d.id])] : g.copies.map((c) => textOf(c.details?.fields[d.id]))))
    .filter(Boolean).join(' ');
}

/** The filter keys a value matches: choice ids, 'yes' / 'no', or '' when it has none. */
function keysOf(v: Value | undefined): string[] {
  const val = v?.value;
  if (!val || val.case === undefined) return [''];
  if (val.case === 'choice') return [val.value];
  if (val.case === 'choices') return val.value.ids.length ? val.value.ids : [''];
  if (val.case === 'bool') return [val.value ? 'yes' : 'no'];
  return [''];
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
  const keys = def.type === 'bool' ? ['yes', 'no'] : def.choices.map((c) => c.id);
  return [...keys, ''].map((key) => ({ key, count: games.filter((g) => gameKeys(g, def).includes(key)).length }));
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
