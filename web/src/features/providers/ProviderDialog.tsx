import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, providerClient } from '../../api/client';
import { FieldHelp } from '../../components/FieldHelp';
import { Alert, Modal } from '../../components/ui';
import type { Provider } from '../../gen/gamevault/v1/provider_pb';
import { SettingField_Kind } from '../../gen/gamevault/v1/source_pb';

/** Configure a provider's credentials, test them and enable it. */
export default function ProviderDialog({ provider, placeholder, onClose, onSaved }: {
  provider: Provider;
  placeholder: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const [settings, setSettings] = useState<Record<string, string>>(() => ({ ...provider.settings }));
  const [enabled, setEnabled] = useState(true);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ tone: 'ok' | 'error'; text: string } | null>(null);

  const test = async () => {
    setBusy(true);
    setResult(null);
    try {
      const res = await providerClient.testProvider({ id: provider.id, settings });
      setResult(res.success ? { tone: 'ok', text: t('providers.testOk') } : { tone: 'error', text: res.message });
    } catch (e) {
      setResult({ tone: 'error', text: errorMessage(e) });
    } finally {
      setBusy(false);
    }
  };

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await providerClient.updateProvider({ id: provider.id, enabled, settings });
      onSaved();
    } catch (err) {
      setResult({ tone: 'error', text: errorMessage(err) });
      setBusy(false);
    }
  };

  return (
    <Modal title={provider.name} onClose={onClose}
      footer={<>
        <button type="button" onClick={test} disabled={busy}>{busy ? t('common.working') : t('sources.test')}</button>
        <span className="spacer" />
        <button type="button" onClick={onClose}>{t('common.cancel')}</button>
        <button type="submit" form="provider-form" className="primary" disabled={busy}>{t('common.save')}</button>
      </>}>
      <form id="provider-form" onSubmit={save}>
        <p className="muted">{t(provider.descriptionKey)}</p>
        <div className="grid">
          {provider.fields.map((f) => (
            <div key={f.key} className="span2 field">
              <label htmlFor={`provider-${f.key}`}>{t(f.labelKey)}{f.required && ' *'}</label>
              <input id={`provider-${f.key}`} type={f.kind === SettingField_Kind.SECRET ? 'password' : 'text'} autoComplete="off" required={f.required}
                value={settings[f.key] ?? ''} onFocus={(e) => settings[f.key] === placeholder && e.target.select()}
                onChange={(e) => setSettings({ ...settings, [f.key]: e.target.value })} />
              {f.helpKey && <FieldHelp text={t(f.helpKey)} url={f.helpUrl} />}
            </div>
          ))}
          <label className="check">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            {t('sources.enabled')}
          </label>
        </div>
        {result && <Alert tone={result.tone}>{result.text}</Alert>}
      </form>
    </Modal>
  );
}
