import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage } from '../../api/client';
import { Alert, Modal } from '../../components/ui';
import {
  CopyKind, KINDS, PHYSICAL_PLATFORMS, STATUSES_BY_KIND, STORE_PLATFORMS, emptyDetails, kindKey, statusKey,
  type CopyDetailsInput,
} from '../../lib/model';

interface Props {
  initial?: CopyDetailsInput;
  /** Shown when the copy is kept in sync by a source. */
  managedBy?: string;
  onSubmit: (d: CopyDetailsInput) => Promise<void>;
  onClose: () => void;
}

/** Add or edit one copy of a game. */
export default function CopyForm({ initial, managedBy, onSubmit, onClose }: Props) {
  const { t } = useTranslation();
  const [d, setD] = useState<CopyDetailsInput>(initial ?? emptyDetails());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const set = <K extends keyof CopyDetailsInput>(k: K, v: CopyDetailsInput[K]) => setD((x) => ({ ...x, [k]: v }));
  const changeKind = (kind: CopyKind) =>
    setD((x) => ({ ...x, kind, status: STATUSES_BY_KIND[kind].includes(x.status) ? x.status : STATUSES_BY_KIND[kind][0] }));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await onSubmit(d);
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
        <button type="submit" form="copy-form" className="primary" disabled={busy}>{busy ? t('common.saving') : t('common.save')}</button>
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
                {t('copy.condition')}
                <input list="copy-conditions" value={d.condition} onChange={(e) => set('condition', e.target.value)} />
                <datalist id="copy-conditions">
                  {['sealed', 'complete', 'caseAndDisc', 'discOnly', 'damaged'].map((c) => <option key={c} value={t(`copy.conditions.${c}`)} />)}
                </datalist>
              </label>
              <label>
                {t('copy.location')}
                <input value={d.location} onChange={(e) => set('location', e.target.value)} placeholder={t('copy.locationPlaceholder')} />
              </label>
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
            {t('copy.edition')}
            <input value={d.edition} onChange={(e) => set('edition', e.target.value)} placeholder="GOTY, Collector's…" />
          </label>
          <label className="span2">
            {t('common.notes')}
            <textarea rows={3} value={d.notes} onChange={(e) => set('notes', e.target.value)} />
          </label>
        </div>
        {error && <Alert tone="error">{error}</Alert>}
      </form>
    </Modal>
  );
}
