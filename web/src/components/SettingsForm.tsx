import { useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { SettingField_Kind, type SettingField } from '../gen/gamevault/v1/source_pb';
import { ACCEPTED } from '../lib/settingsForm';
import { useAction } from '../lib/useAction';
import { FieldHelp } from './FieldHelp';
import { Alert } from './ui';

// The pieces the settings dialogs of sources and providers share: the risk notices a consent field
// shows, the setting inputs, and testing the settings against the real service. A fix to how consent
// is handled (a legal safeguard) is made here once.

type Settings = Record<string, string>;

/** The consent fields among a form's settings. */
export const consentFields = (fields: SettingField[]) => fields.filter((f) => f.kind === SettingField_Kind.CONSENT);

/** One risk notice per consent field: what the service's terms say, and the box that accepts it. */
export function ConsentNotices({ fields, settings, onChange }: {
  fields: SettingField[];
  settings: Settings;
  onChange: (settings: Settings) => void;
}) {
  const { t } = useTranslation();
  return consentFields(fields).map((f) => (
    <section key={f.key} className="risk-notice" role="alert">
      <h3 className="risk-title">{t('settings.riskTitle')}</h3>
      {f.helpKey && <FieldHelp text={t(f.helpKey)} url={f.helpUrl} linkLabel={t('settings.readTerms')} />}
      <label className="check risk-accept">
        <input type="checkbox" checked={settings[f.key] === ACCEPTED}
          onChange={(e) => onChange({ ...settings, [f.key]: e.target.checked ? ACCEPTED : '' })} />
        {t(f.labelKey)}
      </label>
    </section>
  ));
}

/** The inputs of every setting but the consents, each a full-width row of a `.grid`. A secret is a
 *  password input that selects its whole value on focus, so typing replaces the masked one. */
export function SettingInputs({ fields, settings, onChange, idPrefix, renderExtra, helpLinks = true }: {
  fields: SettingField[];
  settings: Settings;
  onChange: (settings: Settings) => void;
  /** Prefix of the inputs' ids, unique to the dialog. */
  idPrefix: string;
  /** Shown between a field's input and its help (the sources' sign-in row). */
  renderExtra?: (f: SettingField) => ReactNode;
  /** Whether the help shows the field's help link (false when renderExtra shows it as a button). */
  helpLinks?: boolean;
}) {
  const { t } = useTranslation();
  return fields.filter((f) => f.kind !== SettingField_Kind.CONSENT).map((f) => {
    const secret = f.kind === SettingField_Kind.SECRET;
    return (
      <div key={f.key} className="span2 field">
        <label htmlFor={`${idPrefix}-${f.key}`}>{t(f.labelKey)}{f.required && ' *'}</label>
        <input
          id={`${idPrefix}-${f.key}`}
          type={secret ? 'password' : 'text'}
          autoComplete="off"
          required={f.required}
          value={settings[f.key] ?? ''}
          onFocus={(e) => secret && e.target.select()}
          onChange={(e) => onChange({ ...settings, [f.key]: e.target.value })}
        />
        {renderExtra?.(f)}
        {f.helpKey && <FieldHelp text={t(f.helpKey)} url={helpLinks ? f.helpUrl : undefined} />}
      </div>
    );
  });
}

/** What testing or saving the settings said last. */
export interface SettingsResult {
  tone: 'ok' | 'error';
  text: string;
}

/** Tests a form's settings with one request to the real service (`request`). `result` is what the
 *  last test, or a failed save (through setResult), said. */
export function useSettingsTest(request: (settings: Settings) => Promise<{ success: boolean; message: string }>) {
  const { t } = useTranslation();
  const [result, setResult] = useState<SettingsResult | null>(null);
  const { busy, run } = useAction({ onError: (text) => setResult({ tone: 'error', text }) });
  /** Tests `settings`; resolves to whether they work. */
  const test = async (settings: Settings) => {
    setResult(null);
    let works = false;
    await run(async () => {
      const res = await request(settings);
      works = res.success;
      setResult(res.success ? { tone: 'ok', text: t('settings.testOk') } : { tone: 'error', text: res.message });
    });
    return works;
  };
  return { testing: busy, result, setResult, test };
}

/** The Test button: "Testing…" while it runs. */
export function TestButton({ testing, disabled, onClick }: { testing: boolean; disabled: boolean; onClick: () => void }) {
  const { t } = useTranslation();
  return <button type="button" onClick={onClick} disabled={disabled}>{testing ? t('settings.testing') : t('settings.test')}</button>;
}

/** The last test's or save's result. */
export function SettingsResultAlert({ result }: { result: SettingsResult | null }) {
  return result && <Alert tone={result.tone}>{result.text}</Alert>;
}
