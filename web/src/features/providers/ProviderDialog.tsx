import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { providerClient } from '../../api/client';
import { ConsentNotices, SettingInputs, SettingsResultAlert, TestButton, consentFields, useSettingsTest } from '../../components/SettingsForm';
import { Modal } from '../../components/ui';
import type { Provider } from '../../gen/gamevault/v1/provider_pb';
import { initialEnabled, type DialogIntent } from '../../lib/providerForm';
import { consentMissing } from '../../lib/settingsForm';
import { useAction } from '../../lib/useAction';

/** Configure a provider's credentials, test them and enable it. */
export default function ProviderDialog({ provider, intent = 'configure', onClose, onSaved }: {
  provider: Provider;
  intent?: DialogIntent;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const [settings, setSettings] = useState<Record<string, string>>(() => ({ ...provider.settings }));
  const [enabled, setEnabled] = useState(() => initialEnabled(provider.enabled, intent));
  const { testing, result, setResult, test } = useSettingsTest((s) => providerClient.testProvider({ id: provider.id, settings: s }));
  // The dialog closes on success, so it stays busy until it goes.
  const saving = useAction({ keepBusyOnSuccess: true, onError: (text) => setResult({ tone: 'error', text }) });
  const busy = testing || saving.busy;
  // A required risk not accepted yet blocks testing and saving (the server refuses it too).
  const noConsent = consentMissing(consentFields(provider.fields), settings);

  const save = (e: FormEvent) => {
    e.preventDefault();
    saving.run(async () => {
      await providerClient.updateProvider({ id: provider.id, enabled, settings });
      onSaved();
    });
  };

  return (
    <Modal title={provider.name} onClose={onClose}
      footer={<>
        <TestButton testing={testing} disabled={busy || noConsent} onClick={() => test(settings)} />
        <span className="spacer" />
        <button type="button" onClick={onClose}>{t('common.cancel')}</button>
        <button type="submit" form="provider-form" className="primary" disabled={busy || (enabled && noConsent)}>{t('common.save')}</button>
      </>}>
      <form id="provider-form" onSubmit={save}>
        <ConsentNotices fields={provider.fields} settings={settings} onChange={setSettings} />
        <p className="muted">{t(provider.descriptionKey)}</p>
        <div className="grid">
          <SettingInputs fields={provider.fields} settings={settings} onChange={setSettings} idPrefix="provider" />
          <label className="check">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            {t('providers.enabled')}
          </label>
        </div>
        <SettingsResultAlert result={result} />
      </form>
    </Modal>
  );
}
