import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, sourceClient } from '../../api/client';
import { FieldHelp } from '../../components/FieldHelp';
import { Alert, Modal, useFormatters } from '../../components/ui';
import { SettingField_Kind, type Source, type SourceType } from '../../gen/gamevault/v1/source_pb';
import { toDate } from '../../lib/model';

interface Props {
  type: SourceType;
  source?: Source;
  onClose: () => void;
  onSaved: (id: string, syncNow: boolean) => void;
  onDeleted: () => void;
}

/** Create or edit a source. Secret settings come back masked; leaving the mask keeps the stored value. */
export default function SourceDialog({ type, source, onClose, onSaved, onDeleted }: Props) {
  const { t } = useTranslation();
  const fmt = useFormatters();
  const [name, setName] = useState(source?.name ?? type.name);
  const [enabled, setEnabled] = useState(source?.enabled ?? true);
  // Some stores (Fanatical) start with manual scans: every request is one the user chose to make.
  const [syncHours, setSyncHours] = useState(source?.syncIntervalHours ?? (type.manualScans ? 0 : 24));
  const [settings, setSettings] = useState<Record<string, string>>(() => ({ ...(source?.settings ?? {}) }));
  const [busy, setBusy] = useState(false);
  const [testing, setTesting] = useState(false);
  const [result, setResult] = useState<{ tone: 'ok' | 'error'; text: string } | null>(null);

  const input = () => ({ type: type.id, name, enabled, syncIntervalHours: syncHours, settings });
  // A required risk not accepted yet blocks testing and saving (the server refuses it too).
  const consents = type.fields.filter((f) => f.kind === SettingField_Kind.CONSENT);
  const consentMissing = consents.some((f) => f.required && settings[f.key] !== 'yes');

  const test = async () => {
    setBusy(true);
    setTesting(true);
    setResult(null);
    try {
      const res = await sourceClient.testSource({ id: source?.id ?? '', source: input() });
      setResult(res.success
        ? { tone: 'ok', text: t('sources.testOk') }
        : { tone: 'error', text: res.message });
    } catch (e) {
      setResult({ tone: 'error', text: errorMessage(e) });
    } finally {
      setBusy(false);
      setTesting(false);
    }
  };

  const save = async (e: FormEvent, syncNow = false) => {
    e.preventDefault();
    setBusy(true);
    try {
      const res = source
        ? await sourceClient.updateSource({ id: source.id, source: input() })
        : await sourceClient.createSource({ source: input() });
      onSaved(res.source!.id, syncNow);
    } catch (err) {
      setResult({ tone: 'error', text: errorMessage(err) });
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!source || !confirm(t('sources.confirmDelete', { name: source.name }))) return;
    const deleteCopies = source.copyCount > 0 && confirm(t('sources.confirmDeleteCopies', { count: source.copyCount }));
    setBusy(true);
    try {
      await sourceClient.deleteSource({ id: source.id, deleteCopies });
      onDeleted();
    } catch (e) {
      setResult({ tone: 'error', text: errorMessage(e) });
      setBusy(false);
    }
  };

  const r = source?.lastSync;

  return (
    <Modal title={source ? t('sources.editTitle', { name: source.name }) : t('sources.addTitle', { type: type.name })} onClose={onClose}
      footer={<>
        {source && <button type="button" className="danger" onClick={remove} disabled={busy}>{t('common.delete')}</button>}
        <button type="button" onClick={test} disabled={busy || consentMissing}>{testing ? t('sources.testing') : t('sources.test')}</button>
        <span className="spacer" />
        <button type="button" onClick={onClose}>{t('common.cancel')}</button>
        {!source && <button type="button" onClick={(e) => save(e, true)} disabled={busy || consentMissing}>{t('sources.saveAndSync')}</button>}
        <button type="submit" form="source-form" className="primary" disabled={busy || consentMissing}>{t('common.save')}</button>
      </>}>
      <form id="source-form" onSubmit={(e) => save(e)}>
        {consents.map((f) => (
          <section key={f.key} className="risk-notice" role="alert">
            <h3 className="risk-title">{t('sources.riskTitle')}</h3>
            {f.helpKey && <FieldHelp text={t(f.helpKey)} url={f.helpUrl} linkLabel={t('sources.readTerms')} />}
            <label className="check risk-accept">
              <input type="checkbox" checked={settings[f.key] === 'yes'}
                onChange={(e) => setSettings({ ...settings, [f.key]: e.target.checked ? 'yes' : '' })} />
              {t(f.labelKey)}
            </label>
          </section>
        ))}
        <p className="muted">{t(type.descriptionKey)}</p>
        {r && (
          <section className={`source-report ${r.success ? '' : 'failed'}`}>
            <h3>{t('sources.lastScan', { when: fmt.dateTime(toDate(r.finishedAt)) })}</h3>
            {r.success ? (
              <p className="small">{t('sources.report', { fetched: r.fetched, added: r.copiesAdded, updated: r.copiesUpdated, games: r.gamesCreated })}</p>
            ) : (
              <Alert tone="error">{r.error}</Alert>
            )}
            {r.warnings.length > 0 && (
              <details>
                <summary>{t('sources.warnings', { count: r.warnings.length })}</summary>
                <ul>{r.warnings.map((w, i) => <li key={i}>{w}</li>)}</ul>
              </details>
            )}
          </section>
        )}
        <div className="grid">
          <label className="span2">
            {t('sources.name')}
            <input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          {type.fields.filter((f) => f.kind !== SettingField_Kind.CONSENT).map((f) => (
            <div key={f.key} className="span2 field">
              <label htmlFor={`source-${f.key}`}>{t(f.labelKey)}{f.required && ' *'}</label>
              <input
                id={`source-${f.key}`}
                type={f.kind === SettingField_Kind.SECRET ? 'password' : 'text'}
                autoComplete="off"
                required={f.required}
                value={settings[f.key] ?? ''}
                onFocus={(e) => f.kind === SettingField_Kind.SECRET && e.target.select()}
                onChange={(e) => setSettings({ ...settings, [f.key]: e.target.value })}
              />
              {f.helpKey && <FieldHelp text={t(f.helpKey)} url={f.helpUrl} />}
            </div>
          ))}
          <label>
            {t('sources.interval')}
            <select value={syncHours} onChange={(e) => setSyncHours(Number(e.target.value))}>
              {[0, 6, 12, 24, 72, 168].map((h) => (
                <option key={h} value={h}>{h === 0 ? t('sources.manual') : t('sources.every', { count: h })}</option>
              ))}
            </select>
          </label>
          <label className="check">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            {t('sources.enabled')}
          </label>
        </div>
        <p className="muted small">{t('sources.secretsNote')}</p>
        {result && <Alert tone={result.tone}>{result.text}</Alert>}
      </form>
    </Modal>
  );
}
