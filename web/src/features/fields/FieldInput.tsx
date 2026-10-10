import { create } from '@bufbuild/protobuf';
import { useEffect, useState, type ChangeEvent, type KeyboardEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, fieldClient } from '../../api/client';
import { Icon } from '../../components/Icon';
import type { FieldDefinition } from '../../gen/gamevault/v1/field_pb';
import { ChoiceListSchema, FieldValueSchema, MoneySchema, type FieldValue } from '../../gen/gamevault/v1/game_pb';
import { numberInput, parseNumber } from '../../lib/fields';
import { amountInput, currencyDigits, parseAmount } from '../../lib/money';
import { usePreferredCurrency } from '../../lib/usePreferences';
import { useAppData } from '../../state/AppData';

/** Values by field id, as the game and the copy details carry them. */
export type FieldValues = { [id: string]: FieldValue };

type Inner = FieldValue['value'];

/** A value message from its oneof; `{ case: undefined }` is the empty value that removes it. */
const make = (value: Inner): FieldValue => create(FieldValueSchema, { value });
const EMPTY = make({ case: undefined });

/** Whether a value holds nothing worth saving: no case, blank text or no chosen ids. */
export function isEmptyValue(v: FieldValue | undefined): boolean {
  const val = v?.value;
  if (!val || val.case === undefined) return true;
  if (val.case === 'text') return val.value.trim() === '';
  if (val.case === 'choices') return val.value.ids.length === 0;
  return false;
}

/** The values without the empty ones: the server is never sent an empty value. */
export function cleanFields(values: FieldValues): FieldValues {
  return Object.fromEntries(Object.entries(values).filter(([, v]) => !isEmptyValue(v)));
}

/** A value as a comparable string (bigints and nested messages included). */
function valueKey(v: FieldValue | undefined): string {
  const val = v?.value;
  if (!val || isEmptyValue(v)) return '';
  switch (val.case) {
    case 'money': return `money:${val.value.amountMinor}:${val.value.currency}`;
    case 'choices': return `choices:${val.value.ids.join(',')}`;
    default: return `${val.case}:${String(val.value)}`;
  }
}

/** Whether two value maps hold the same non-empty values. */
export function sameFields(a: FieldValues, b: FieldValues): boolean {
  const key = (m: FieldValues) => JSON.stringify(Object.entries(cleanFields(m)).map(([id, v]) => [id, valueKey(v)]).sort());
  return key(a) === key(b);
}

/** The custom fields as a grid of labelled controls, one per definition, in the definitions' order. */
export function FieldInputs({ defs, values, onChange, className }: {
  defs: FieldDefinition[];
  values: FieldValues;
  /** One field's new value (an empty value when it was removed); the parent merges it into its
   *  latest map, since a new list value arrives after a round trip. */
  onChange: (id: string, v: FieldValue) => void;
  className?: string;
}) {
  if (defs.length === 0) return null;
  return (
    <div className={`grid field-inputs ${className ?? ''}`}>
      {defs.map((d) => (
        <FieldInput key={d.id} def={d} value={values[d.id]} onChange={(v) => onChange(d.id, v)} />
      ))}
    </div>
  );
}

/** One custom field with its label: the control depends on the field's type. */
export function FieldInput({ def, value, onChange }: {
  def: FieldDefinition;
  value: FieldValue | undefined;
  onChange: (v: FieldValue) => void;
}) {
  const val = value?.value;
  switch (def.type) {
    case 'text':
      return (
        <label>
          {def.name}
          <input maxLength={250} value={val?.case === 'text' ? val.value : ''}
            onChange={(e) => onChange(make({ case: 'text', value: e.target.value }))} />
        </label>
      );
    case 'longtext':
      return (
        <label className="span2">
          {def.name}
          <textarea rows={3} maxLength={10000} value={val?.case === 'text' ? val.value : ''}
            onChange={(e) => onChange(make({ case: 'text', value: e.target.value }))} />
        </label>
      );
    case 'bool':
      return <BoolInput def={def} value={val?.case === 'bool' ? val.value : undefined} onChange={onChange} />;
    case 'number':
      return <NumberInput def={def} value={val?.case === 'number' ? val.value : undefined} onChange={onChange} />;
    case 'money':
      return <MoneyInput def={def} value={val?.case === 'money' ? val.value : undefined} onChange={onChange} />;
    case 'date':
      return <DateInput def={def} value={val?.case === 'date' ? val.value : ''} onChange={onChange} />;
    case 'duration':
      return <DurationInput def={def} value={val?.case === 'minutes' ? val.value : undefined} onChange={onChange} />;
    case 'list':
      return <ListInput def={def} value={val?.case === 'choice' ? val.value : ''} onChange={onChange} />;
    case 'multilist':
      return <MultiListInput def={def} ids={val?.case === 'choices' ? val.value.ids : []} onChange={onChange} />;
    default:
      return null;
  }
}

/** A control made of several elements: a labelled group instead of a <label>. */
function Group({ def, wide, error, children }: { def: FieldDefinition; wide?: boolean; error?: string; children: ReactNode }) {
  return (
    <div className={`field${wide ? ' span2' : ''}`} role="group" aria-label={def.name}>
      <span className="field-label">{def.name}</span>
      {children}
      {error && <span className="help error">{error}</span>}
    </div>
  );
}

/**
 * Text typed in an input that stands for a value: kept while it still means the current value (so
 * "3,5" is not rewritten while typing), replaced when the value changes from outside (Discard).
 */
function useDraft(key: string, format: () => string, keyOfText: (text: string) => string) {
  const [text, setText] = useState(format);
  useEffect(() => {
    setText((t) => (keyOfText(t) === key ? t : format()));
    // Only a new value resets the text; format and keyOfText are recreated on every render.
  }, [key]);
  return [text, setText] as const;
}

/** Yes / No / — (no value), like the other segmented controls. */
function BoolInput({ def, value, onChange }: { def: FieldDefinition; value: boolean | undefined; onChange: (v: FieldValue) => void }) {
  const { t } = useTranslation();
  const options: [string, boolean | undefined][] = [[t('fields.yes'), true], [t('fields.no'), false], ['—', undefined]];
  return (
    <Group def={def}>
      <div className="segmented">
        {options.map(([label, b]) => (
          <button type="button" key={label} className={value === b ? 'active' : ''} aria-pressed={value === b}
            aria-label={b === undefined ? t('fields.noValue') : undefined} title={b === undefined ? t('fields.noValue') : undefined}
            onClick={() => onChange(b === undefined ? EMPTY : make({ case: 'bool', value: b }))}>
            {label}
          </button>
        ))}
      </div>
    </Group>
  );
}

/** A number with the field's decimals; the unit follows the input. Invalid text keeps the last value. */
function NumberInput({ def, value, onChange }: { def: FieldDefinition; value: bigint | undefined; onChange: (v: FieldValue) => void }) {
  const { t } = useTranslation();
  const format = () => (value === undefined ? '' : numberInput(value, def.decimals));
  const keyOf = (s: string) => (s.trim() === '' ? '' : String(parseNumber(s, def.decimals)));
  const [text, setText] = useDraft(value === undefined ? '' : String(value), format, keyOf);
  const parsed = parseNumber(text, def.decimals);
  const invalid = text.trim() !== '' && parsed === null;
  const change = (s: string) => {
    setText(s);
    const n = parseNumber(s, def.decimals);
    if (s.trim() === '') onChange(EMPTY);
    else if (n !== null) onChange(make({ case: 'number', value: n }));
  };
  return (
    <label>
      {def.name}
      <span className="row tight">
        <input inputMode="decimal" value={text} aria-invalid={invalid} onChange={(e) => change(e.target.value)}
          onBlur={() => { if (!invalid) setText(format()); }} />
        {def.unit && <span className="field-unit">{def.unit}</span>}
      </span>
      {invalid && <span className="help error">{t('fields.invalidNumber')}</span>}
    </label>
  );
}

/** An amount in the field's currency, or in the default one when the field has none. */
function MoneyInput({ def, value, onChange }: {
  def: FieldDefinition;
  value: { amountMinor: bigint; currency: string } | undefined;
  onChange: (v: FieldValue) => void;
}) {
  const { t, i18n } = useTranslation();
  const preferred = usePreferredCurrency(i18n.language);
  const currency = def.currency || value?.currency || preferred;
  const digits = currencyDigits(currency);
  const format = () => (value ? amountInput(value.amountMinor, currency, i18n.language) || '0' : '');
  const keyOf = (s: string) => (s.trim() === '' ? '' : String(parseAmount(s, digits)));
  const [text, setText] = useDraft(value ? String(value.amountMinor) : '', format, keyOf);
  const invalid = text.trim() !== '' && parseAmount(text, digits) === null;
  const change = (s: string) => {
    setText(s);
    const minor = parseAmount(s, digits);
    if (s.trim() === '') onChange(EMPTY);
    else if (minor !== null) onChange(make({ case: 'money', value: create(MoneySchema, { amountMinor: minor, currency }) }));
  };
  return (
    <label>
      {def.name}
      <span className="row tight">
        <input inputMode="decimal" value={text} aria-invalid={invalid} onChange={(e) => change(e.target.value)}
          onBlur={() => { if (!invalid) setText(format()); }} placeholder={amountInput(2995n, currency, i18n.language)} />
        <span className="field-unit">{currency}</span>
      </span>
      {invalid && <span className="help error">{t('fields.invalidNumber')}</span>}
    </label>
  );
}

const pad = (n: number) => String(n).padStart(2, '0');

/** Year, then an optional month, then an optional day (only with a month). */
function DateInput({ def, value, onChange }: { def: FieldDefinition; value: string; onChange: (v: FieldValue) => void }) {
  const { t, i18n } = useTranslation();
  const [y = '', m = '', d = ''] = value ? value.split('-') : [];
  const [year, setYear] = useDraft(value, () => y, (s) => (/^\d{4}$/.test(s) ? [s, m, d].filter(Boolean).join('-') : s === '' ? '' : '?'));
  const invalid = year !== '' && !/^\d{4}$/.test(year);
  const emit = (yy: string, mm: string, dd: string) => {
    if (!yy) onChange(EMPTY);
    else onChange(make({ case: 'date', value: [yy, mm, mm && dd].filter(Boolean).join('-') }));
  };
  const changeYear = (s: string) => {
    setYear(s);
    if (s === '') emit('', '', '');
    else if (/^\d{4}$/.test(s)) emit(s, m, d && Number(d) <= daysIn(s, m) ? d : '');
  };
  const months = Array.from({ length: 12 }, (_, i) => i + 1);
  const monthName = (n: number) => new Date(2000, n - 1, 1).toLocaleDateString(i18n.language, { month: 'long' });
  const days = m ? Array.from({ length: daysIn(y || '2000', m) }, (_, i) => i + 1) : [];
  return (
    <Group def={def} error={invalid ? t('fields.invalidYear') : undefined}>
      <span className="field-date">
        <input className="field-year" inputMode="numeric" maxLength={4} value={year} placeholder={t('fields.year')}
          aria-label={t('fields.year')} aria-invalid={invalid} onChange={(e) => changeYear(e.target.value.trim())} />
        <select value={m} aria-label={t('fields.month')} disabled={!y}
          onChange={(e) => { const mm = e.target.value; emit(y, mm, d && mm && Number(d) <= daysIn(y, mm) ? d : ''); }}>
          <option value="">{t('fields.month')}</option>
          {months.map((n) => <option key={n} value={pad(n)}>{monthName(n)}</option>)}
        </select>
        <select value={d} aria-label={t('fields.day')} disabled={!m} onChange={(e) => emit(y, m, e.target.value)}>
          <option value="">{t('fields.day')}</option>
          {days.map((n) => <option key={n} value={pad(n)}>{n}</option>)}
        </select>
      </span>
    </Group>
  );
}

/** Days in a month ("2024", "02" → 29). */
function daysIn(year: string, month: string): number {
  if (!month) return 31;
  return new Date(Number(year), Number(month), 0).getDate();
}

/** Hours and minutes, stored as minutes. */
function DurationInput({ def, value, onChange }: { def: FieldDefinition; value: bigint | undefined; onChange: (v: FieldValue) => void }) {
  const { t } = useTranslation();
  const split = (v: bigint | undefined): [string, string] =>
    (v === undefined ? ['', ''] : [v >= 60n ? String(v / 60n) : '', String(v % 60n)]);
  const parse = (h: string, m: string): bigint | null | undefined => {
    if (h.trim() === '' && m.trim() === '') return undefined;
    if (!/^\d*$/.test(h.trim()) || !/^\d*$/.test(m.trim())) return null;
    return BigInt(h.trim() || '0') * 60n + BigInt(m.trim() || '0');
  };
  const [hm, setHm] = useDraft(value === undefined ? '' : String(value), () => split(value).join(':'), (s) => {
    const [h = '', m = ''] = s.split(':');
    const v = parse(h, m);
    return v === undefined ? '' : String(v);
  });
  const [h = '', m = ''] = hm.split(':');
  const invalid = parse(h, m) === null;
  const change = (hh: string, mm: string) => {
    setHm(`${hh}:${mm}`);
    const v = parse(hh, mm);
    if (v === undefined) onChange(EMPTY);
    else if (v !== null) onChange(make({ case: 'minutes', value: v }));
  };
  return (
    <Group def={def} error={invalid ? t('fields.invalidNumber') : undefined}>
      <span className="field-duration">
        <input inputMode="numeric" value={h} aria-label={t('fields.hoursLabel')} aria-invalid={invalid}
          onChange={(e) => change(e.target.value.replace(':', ''), m)} />
        <span className="field-unit">{t('fields.hours')}</span>
        <input inputMode="numeric" value={m} aria-label={t('fields.minutesLabel')} aria-invalid={invalid}
          onChange={(e) => change(h, e.target.value.replace(':', ''))} />
        <span className="field-unit">{t('fields.minutes')}</span>
      </span>
    </Group>
  );
}

/** One value of a list: a segmented control for a few values, a select for more. */
function ListInput({ def, value, onChange }: { def: FieldDefinition; value: string; onChange: (v: FieldValue) => void }) {
  const { t } = useTranslation();
  const pick = (id: string) => onChange(id ? make({ case: 'choice', value: id }) : EMPTY);
  if (def.choices.length <= 4) {
    return (
      <Group def={def}>
        <div className="segmented">
          {[...def.choices, { id: '', name: '—' }].map((c) => (
            <button type="button" key={c.id} className={value === c.id ? 'active' : ''} aria-pressed={value === c.id}
              aria-label={c.id ? undefined : t('fields.noValue')} title={c.id ? undefined : t('fields.noValue')} onClick={() => pick(c.id)}>
              {c.name}
            </button>
          ))}
        </div>
      </Group>
    );
  }
  return (
    <label>
      {def.name}
      <select value={value} onChange={(e) => pick(e.target.value)}>
        <option value="">{t('fields.noValue')}</option>
        {def.choices.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
      </select>
    </label>
  );
}

/** Several values of a list: chips, and an input that picks another value or adds a new one. */
function MultiListInput({ def, ids, onChange }: { def: FieldDefinition; ids: string[]; onChange: (v: FieldValue) => void }) {
  const { t } = useTranslation();
  const { reloadFields } = useAppData();
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const set = (next: string[]) => onChange(next.length ? make({ case: 'choices', value: create(ChoiceListSchema, { ids: next }) }) : EMPTY);
  const others = def.choices.filter((c) => !ids.includes(c.id));
  const listId = `field-${def.id}-choices`;

  const add = async (name: string) => {
    const clean = name.trim();
    if (!clean) return;
    const known = def.choices.find((c) => c.name.toLowerCase() === clean.toLowerCase());
    if (known) {
      if (!ids.includes(known.id)) set([...ids, known.id]);
      setText('');
      return;
    }
    // A new value joins the field's list first; the server reuses a name that differs only in case.
    setBusy(true);
    setError('');
    try {
      const res = await fieldClient.addChoice({ fieldId: def.id, name: clean });
      await reloadFields();
      const id = res.choice!.id;
      if (!ids.includes(id)) set([...ids, id]);
      setText('');
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  // A pick from the datalist arrives as a replacement of the whole text, not as typing.
  const change = (e: ChangeEvent<HTMLInputElement>) => {
    const s = e.target.value;
    const native = e.nativeEvent as InputEvent;
    const picked = !(native instanceof InputEvent) || native.inputType === 'insertReplacementText';
    if (picked && others.some((c) => c.name === s)) {
      void add(s);
      return;
    }
    setText(s);
  };
  const keyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== 'Enter') return;
    e.preventDefault(); // Enter adds the value; it never submits the form around it.
    void add(text);
  };

  return (
    <Group def={def} wide error={error}>
      {ids.length > 0 && (
        <ul className="field-chips">
          {ids.map((id) => (
            <li key={id} className="field-chip">
              {def.choices.find((c) => c.id === id)?.name ?? id}
              <button type="button" className="field-chip-remove" aria-label={t('fields.removeChoice', { name: def.choices.find((c) => c.id === id)?.name ?? id })}
                onClick={() => set(ids.filter((x) => x !== id))}><Icon name="close" size={14} /></button>
            </li>
          ))}
        </ul>
      )}
      <input list={listId} value={text} maxLength={60} readOnly={busy} placeholder={t('fields.addChoice')}
        aria-label={`${def.name}: ${t('fields.addChoice')}`} onChange={change} onKeyDown={keyDown} />
      <datalist id={listId}>{others.map((c) => <option key={c.id} value={c.name} />)}</datalist>
    </Group>
  );
}
