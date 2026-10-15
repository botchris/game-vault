import { create } from '@bufbuild/protobuf';
import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import SystemPicker from '../../components/SystemPicker';
import { Alert, Modal } from '../../components/ui';
import { MoneySchema } from '../../gen/gamevault/v1/game_pb';
import { systemOf } from '../../lib/editions';
import { findSystem } from '../../lib/systems';
import { copyValuesFor } from '../../lib/fields';
import { amountInput, currencyList } from '../../lib/money';
import {
  CopyKind, KINDS, STATUSES_BY_KIND, STORE_PLATFORMS, emptyDetails, kindKey, statusKey, type CopyDetailsInput,
} from '../../lib/model';
import { usePreferredCurrency } from '../../lib/usePreferences';
import { useAppData } from '../../state/AppData';
import { FieldInputs, applyField, cleanFields, useFieldValidity } from '../fields/FieldInput';
import { ContentsToggles, GradeSelect, LocationInput } from './PhysicalInputs';
import { useAmountReading } from '../../lib/useAmountReading';
import { useAction } from '../../lib/useAction';

interface Props {
  initial?: CopyDetailsInput;
  /** Shown when the copy is kept in sync by a source. */
  managedBy?: string;
  /** The system the copy's source gives it, if any: what "automatic" means for it. */
  sourceSystem?: string;
  onSubmit: (d: CopyDetailsInput) => Promise<void>;
  onClose: () => void;
}

/** Add or edit one copy of a game. */
export default function CopyForm({ initial, managedBy, sourceSystem, onSubmit, onClose }: Props) {
  const { t, i18n } = useTranslation();
  const { fields, systems } = useAppData();
  const systemNames = systems.map((s) => s.name);
  const preferred = usePreferredCurrency(i18n.language);
  const [d, setD] = useState<CopyDetailsInput>(initial ?? emptyDetails());
  // The dialog closes on success, so it stays busy until it goes.
  const { busy, error, run } = useAction({ keepBusyOnSuccess: true });
  const [amount, setAmount] = useState(() => (d.price?.amountMinor ? amountInput(d.price.amountMinor, d.price.currency, i18n.language) : ''));
  const [chosenCurrency, setCurrency] = useState(d.price?.currency ?? '');
  // A new price uses the default currency until the user picks another one.
  const currency = chosenCurrency || preferred;
  const price = useAmountReading(currency, amount);
  const amountInvalid = price.invalid;
  // A custom field holding invalid text (a number, a year) blocks Save until it is fixed or cleared.
  const [fieldsInvalid, reportField] = useFieldValidity();
  // What an empty System means: the source's system, else the platform's (it follows the platform as
  // it is typed). With an override stored, the API does not say what the source gives, so automatic
  // is unknown (null) and the form names no system it may get wrong.
  const automatic = initial?.system ? null : sourceSystem || systemOf(d.platform, systemNames);
  // Whether the System field was touched: a stored override is kept as it is unless it was edited,
  // since the source's system (what automatic would give) is not always known here.
  const [systemEdited, setSystemEdited] = useState(false);

  // The copy fields that apply to the chosen kind (a field without kinds applies to all).
  const defs = fields.filter((f) => f.scope === 'copy' && (!f.kinds.length || f.kinds.includes(d.kind)));

  const set = <K extends keyof CopyDetailsInput>(k: K, v: CopyDetailsInput[K]) => setD((x) => ({ ...x, [k]: v }));
  const changeKind = (kind: CopyKind) =>
    setD((x) => ({ ...x, kind, status: STATUSES_BY_KIND[kind].includes(x.status) ? x.status : STATUSES_BY_KIND[kind][0] }));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (amountInvalid || fieldsInvalid) return;
    await run(async () => {
      // The server replaces the copy's values: drop only those of known fields that no longer apply
      // after a kind change; values of fields this page does not know are kept, never erased.
      const values = cleanFields(copyValuesFor(d.fields, fields, d.kind));
      // The system goes back to automatic when it is emptied, or edited to what automatic gives
      // (when that is known). A typed alias counts as its system ("Super Famicom" is "SNES").
      const typed = d.system.trim();
      const resolved = findSystem(systems, typed)?.name ?? typed;
      const system = typed === '' || (systemEdited && automatic !== null && systemOf(resolved, systemNames) === automatic) ? '' : typed;
      await onSubmit({ ...d, system, price: price.minor ? create(MoneySchema, { amountMinor: price.minor, currency }) : undefined, fields: values });
    });
  };

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
          {d.kind === CopyKind.PHYSICAL ? (
            <SystemPicker label={t('copy.platform')} value={d.platform} onChange={(v) => set('platform', v)} placeholder="PS4, Xbox 360…" autoFocus />
          ) : (
            <label>
              {t('copy.platform')}
              <input list="copy-platforms" value={d.platform} onChange={(e) => set('platform', e.target.value)}
                placeholder="Steam, Ubisoft Connect…" autoFocus />
              <datalist id="copy-platforms">{STORE_PLATFORMS.map((p) => <option key={p} value={p} />)}</datalist>
            </label>
          )}
          <SystemPicker label={t('copy.system')} value={d.system} placeholder={automatic ?? t('copy.systemAutomatic')}
            onChange={(v) => { setSystemEdited(true); set('system', v); }}>
            <span className="help">{automatic === null ? t('copy.systemHelpOverride') : t('copy.systemHelp', { system: automatic })}</span>
          </SystemPicker>
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
              <GradeSelect value={d.grade} onChange={(g) => set('grade', g)} />
              <LocationInput value={d.location} onChange={(l) => set('location', l)} />
              <ContentsToggles className="span2" value={d.contents} onChange={(c) => set('contents', c)} />
              <label>
                {t('copy.barcode')}
                <input className="mono" inputMode="numeric" value={d.barcode} onChange={(e) => set('barcode', e.target.value)} placeholder="2 001234 567893" />
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
                onChange={(e) => setAmount(e.target.value)} placeholder={price.placeholder} />
              <select className="currency" value={currency} onChange={(e) => setCurrency(e.target.value)} aria-label={t('copy.currency')}>
                {currencyList().map((c) => <option key={c} value={c}>{c}</option>)}
              </select>
            </span>
            {amountInvalid && <span className="help error">{t('copy.priceInvalid')}</span>}
          </label>
          <label>
            {t('copy.edition')}
            <input value={d.edition} onChange={(e) => set('edition', e.target.value)} placeholder={t('copy.editionPlaceholder')} />
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
