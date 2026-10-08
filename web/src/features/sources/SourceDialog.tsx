import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, sourceClient } from '../../api/client';
import { FieldHelp } from '../../components/FieldHelp';
import { Alert, Modal } from '../../components/ui';
import { SettingField_Kind, type Source, type SourceType } from '../../gen/gamevault/v1/source_pb';

interface Props {
  type: SourceType;
  source?: Source;
  onClose: () => void;
  onSaved: (id: string, syncNow: boolean) => void;
}

/** Create or edit a source. Secret settings come back masked; leaving the mask keeps the stored value. */
const TEST_MESSAGES: Record<string, string> = { orders: 'sources.testOkOrders', items: 'sources.testOkItems', copies: 'sources.testOkCopies' };

export default function SourceDialog({ type, source, onClose, onSaved }: Props) {
  const { t } = useTranslation();
  const [name, setName] = useState(source?.name ?? type.name);
  const [enabled, setEnabled] = useState(source?.enabled ?? true);
  const [syncHours, setSyncHours] = useState(source?.syncIntervalHours ?? 24);
  const [settings, setSettings] = useState<Record<string, string>>(() => ({ ...(source?.settings ?? {}) }));
  const [busy, setBusy] = useState(false);
  const [testing, setTesting] = useState(false);
  const [result, setResult] = useState<{ tone: 'ok' | 'error'; text: string } | null>(null);

  const input = () => ({ type: type.id, name, enabled, syncIntervalHours: syncHours, settings });

  const test = async () => {
    setBusy(true);
    setTesting(true);
    setResult(null);
    try {
      const res = await sourceClient.testSource({ id: source?.id ?? '', source: input() });
      setResult(res.success
        ? { tone: 'ok', text: t(TEST_MESSAGES[res.countUnit] ?? 'sources.testOkCopies', { count: res.fetched }) }
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

  return (
    <Modal title={source ? t('sources.editTitle', { name: source.name }) : t('sources.addTitle', { type: type.name })} onClose={onClose}
      footer={<>
        <button type="button" onClick={test} disabled={busy}>{testing ? t('sources.testing') : t('sources.test')}</button>
        <span className="spacer" />
        <button type="button" onClick={onClose}>{t('common.cancel')}</button>
        {!source && <button type="button" onClick={(e) => save(e, true)} disabled={busy}>{t('sources.saveAndSync')}</button>}
        <button type="submit" form="source-form" className="primary" disabled={busy}>{t('common.save')}</button>
      </>}>
      <form id="source-form" onSubmit={(e) => save(e)}>
        <p className="muted">{t(type.descriptionKey)}</p>
        <div className="grid">
          <label className="span2">
            {t('sources.name')}
            <input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          {type.fields.map((f) => (
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
