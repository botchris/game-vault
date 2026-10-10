import { create } from '@bufbuild/protobuf';
import { useMemo, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage } from '../../api/client';
import { Alert, Modal } from '../../components/ui';
import { CopyGrade, MoneySchema } from '../../gen/gamevault/v1/game_pb';
import { copyValuesFor } from '../../lib/fields';
import { amountInput, currencyDigits, currencyList, parseAmount } from '../../lib/money';
import {
  CONTENTS, CopyKind, GRADES, KINDS, PHYSICAL_PLATFORMS, STATUSES_BY_KIND, STORE_PLATFORMS, contentKey, emptyDetails,
  gradeKey, kindKey, statusKey, usedLocations, type CopyDetailsInput,
} from '../../lib/model';
import { usePreferredCurrency } from '../../lib/usePreferences';
import { useAppData } from '../../state/AppData';
import { FieldInputs, applyField, cleanFields, useFieldValidity } from '../fields/FieldInput';

interface Props {
  initial?: CopyDetailsInput;
  /** Shown when the copy is kept in sync by a source. */
  managedBy?: string;
  onSubmit: (d: CopyDetailsInput) => Promise<void>;
  onClose: () => void;
}

/** Add or edit one copy of a game. */
export default function CopyForm({ initial, managedBy, onSubmit, onClose }: Props) {
  const { t, i18n } = useTranslation();
  const { games, fields } = useAppData();
  const locations = useMemo(() => usedLocations(games), [games]);
  const preferred = usePreferredCurrency(i18n.language);
  const [d, setD] = useState<CopyDetailsInput>(initial ?? emptyDetails());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [amount, setAmount] = useState(() => (d.price?.amountMinor ? amountInput(d.price.amountMinor, d.price.currency, i18n.language) : ''));
  const [chosenCurrency, setCurrency] = useState(d.price?.currency ?? '');
  // A new price uses the default currency until the user picks another one.
  const currency = chosenCurrency || preferred;
  const minor = amount.trim() === '' ? 0n : parseAmount(amount, currencyDigits(currency));
  const amountInvalid = minor === null;
  // A custom field holding invalid text (a number, a year) blocks Save until it is fixed or cleared.
  const [fieldsInvalid, reportField] = useFieldValidity();

  // The copy fields that apply to the chosen kind (a field without kinds applies to all).
  const defs = fields.filter((f) => f.scope === 'copy' && (!f.kinds.length || f.kinds.includes(d.kind)));

  const set = <K extends keyof CopyDetailsInput>(k: K, v: CopyDetailsInput[K]) => setD((x) => ({ ...x, [k]: v }));
  const changeKind = (kind: CopyKind) =>
    setD((x) => ({ ...x, kind, status: STATUSES_BY_KIND[kind].includes(x.status) ? x.status : STATUSES_BY_KIND[kind][0] }));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (amountInvalid || fieldsInvalid) return;
    setBusy(true);
    setError('');
    try {
      // The server replaces the copy's values: drop only those of known fields that no longer apply
      // after a kind change; values of fields this page does not know are kept, never erased.
      const values = cleanFields(copyValuesFor(d.fields, fields, d.kind));
      await onSubmit({ ...d, price: minor ? create(MoneySchema, { amountMinor: minor, currency }) : undefined, fields: values });
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  };

  const platforms = d.kind === CopyKind.PHYSICAL ? PHYSICAL_PLATFORMS : STORE_PLATFORMS;

  return (
    <Modal title={initial ? t('copy.edit') : t('copy.add')} onClose={onClose}
      footer={<>
        <span className="spacer" />
        <button type="button" onClick={onClose}>{t('common.cancel')}</button>
        <button type="submit" form="copy-form" className="primary" disabled={busy || amountInvalid || fieldsInvalid}>{busy ? t('common.saving') : t('common.save')}</button>
      </>}>
      <form id="copy-form" onSubmit={submit}>
        {managedBy && <p className="muted small">{t('copy.managedBy', { source: managedBy })}</p>}
        <div className="segmented">
          {KINDS.map((k) => (
            <button type="button" key={k} className={d.kind === k ? 'active' : ''} onClick={() => changeKind(k)}>{t(`kind.${kindKey(k)}`)}</button>
          ))}
        </div>
        <div className="grid">
          <label>
            {t('copy.platform')}
            <input list="copy-platforms" value={d.platform} onChange={(e) => set('platform', e.target.value)}
              placeholder={d.kind === CopyKind.PHYSICAL ? 'PS4, Xbox 360…' : 'Steam, Ubisoft Connect…'} autoFocus />
            <datalist id="copy-platforms">{platforms.map((p) => <option key={p} value={p} />)}</datalist>
          </label>
          <label>
            {t('copy.status')}
            <select value={d.status} onChange={(e) => set('status', Number(e.target.value))}>
              {STATUSES_BY_KIND[d.kind].map((s) => <option key={s} value={s}>{t(`status.${statusKey(s)}`)}</option>)}
            </select>
          </label>
          {d.kind === CopyKind.KEY && (
            <>
              <label className="span2">
                {t('copy.key')}
                <input className="mono" value={d.key} onChange={(e) => set('key', e.target.value)} placeholder="XXXXX-XXXXX-XXXXX" />
              </label>
              <label>
                {t('copy.redeemBy')}
                <input type="date" value={d.redeemBy} onChange={(e) => set('redeemBy', e.target.value)} />
              </label>
            </>
          )}
          {d.kind === CopyKind.PHYSICAL && (
            <>
              <label>
                {t('copy.grade')}
                <select value={d.grade} onChange={(e) => set('grade', Number(e.target.value))}>
                  <option value={CopyGrade.UNSPECIFIED}>{t('grade.unspecified')}</option>
                  {GRADES.map((g) => <option key={g} value={g}>{t(`grade.${gradeKey(g)}`)}</option>)}
                </select>
              </label>
              <label>
                {t('copy.location')}
                <input list="copy-locations" value={d.location} onChange={(e) => set('location', e.target.value)} placeholder={t('copy.locationPlaceholder')} />
                <datalist id="copy-locations">{locations.map((l) => <option key={l} value={l} />)}</datalist>
              </label>
              <div className="span2 field">
                <span className="field-label">{t('copy.contents')}</span>
                <div className="toggles" role="group" aria-label={t('copy.contents')}>
                  {CONTENTS.map((c) => {
                    const on = d.contents.includes(c);
                    return (
                      <button type="button" key={c} className={on ? 'toggle on' : 'toggle'} aria-pressed={on}
                        onClick={() => set('contents', on ? d.contents.filter((x) => x !== c) : [...d.contents, c])}>
                        {t(`content.${contentKey(c)}`)}
                      </button>
                    );
                  })}
                </div>
              </div>
              <label>
                {t('copy.barcode')}
                <input className="mono" inputMode="numeric" value={d.barcode} onChange={(e) => set('barcode', e.target.value)} placeholder="5 026555 255042" />
              </label>
            </>
          )}
          <label>
            {t('copy.origin')}
            <input value={d.origin} onChange={(e) => set('origin', e.target.value)} placeholder={t('copy.originPlaceholder')} />
          </label>
          <label>
            {t('copy.acquiredOn')}
            <input type="date" value={d.acquiredOn} onChange={(e) => set('acquiredOn', e.target.value)} />
          </label>
          <label>
            {t('copy.price')}
            <span className="row tight">
              <input inputMode="decimal" value={amount} aria-invalid={amountInvalid}
                onChange={(e) => setAmount(e.target.value)} placeholder={amountInput(2995n, currency, i18n.language)} />
              <select className="currency" value={currency} onChange={(e) => setCurrency(e.target.value)} aria-label={t('copy.currency')}>
                {currencyList().map((c) => <option key={c} value={c}>{c}</option>)}
              </select>
            </span>
            {amountInvalid && <span className="help error">{t('copy.priceInvalid')}</span>}
          </label>
          <label>
            {t('copy.edition')}
            <input value={d.edition} onChange={(e) => set('edition', e.target.value)} placeholder="GOTY, Collector's…" />
          </label>
          <label className="span2">
            {t('common.notes')}
            <textarea rows={3} value={d.notes} onChange={(e) => set('notes', e.target.value)} />
          </label>
        </div>
        <FieldInputs defs={defs} values={d.fields} onValidity={reportField}
          onChange={(id, update) => setD((x) => ({ ...x, fields: applyField(x.fields, id, update) }))} />
        {error && <Alert tone="error">{error}</Alert>}
      </form>
    </Modal>
  );
}
