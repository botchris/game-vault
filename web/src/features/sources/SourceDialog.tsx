import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, sourceClient } from '../../api/client';
import { Icon } from '../../components/Icon';
import { ConsentNotices, SettingInputs, SettingsResultAlert, TestButton, consentFields, useSettingsTest } from '../../components/SettingsForm';
import { Alert, Modal, useFormatters } from '../../components/ui';
import type { SettingField, Source, SourceType } from '../../gen/gamevault/v1/source_pb';
import { connect, ConnectorError, detect, type Connector, type Recipe } from '../../lib/connector';
import { toDate } from '../../lib/model';
import { consentMissing } from '../../lib/settingsForm';
import { useAction } from '../../lib/useAction';
import { useAppData } from '../../state/AppData';

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
  const input = (with_ = settings) => ({ type: type.id, name, enabled, syncIntervalHours: syncHours, settings: with_ });
  const { testing, result, setResult, test } = useSettingsTest((with_) => sourceClient.testSource({ id: source?.id ?? '', source: input(with_) }));
  // Saving and deleting close the dialog on success, so it stays busy until it goes.
  const action = useAction({ keepBusyOnSuccess: true, onError: (text) => setResult({ tone: 'error', text }) });
  const busy = testing || action.busy;
  // The Game Vault Connector extension, when installed and enabled on this address.
  const [connector, setConnector] = useState<Connector | null>(null);
  const [connecting, setConnecting] = useState<{ key: string; cancel: () => void } | null>(null);
  useEffect(() => { detect().then(setConnector); }, []);
  // Closing the dialog cancels a sign-in still running (its tab closes) and drops its value.
  const mounted = useRef(true);
  const running = useRef<(() => void) | null>(null);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      running.current?.();
    };
  }, []);

  const recipeOf = (f: SettingField): Recipe | null => {
    try { return f.signIn ? JSON.parse(f.signIn) as Recipe : null; } catch { return null; }
  };
  // A required risk not accepted yet blocks testing and saving (the server refuses it too).
  const noConsent = consentMissing(consentFields(type.fields), settings);
  // While a sign-in runs the field is about to change: testing, saving and deleting wait for it.
  const locked = busy || !!connecting;

  const saveWith = (with_: Record<string, string>, syncNow = false) => action.run(async () => {
    const res = source
      ? await sourceClient.updateSource({ id: source.id, source: input(with_) })
      : await sourceClient.createSource({ source: input(with_) });
    onSaved(res.source!.id, syncNow);
  });

  const save = async (e: FormEvent, syncNow = false) => {
    e.preventDefault();
    await saveWith(settings, syncNow);
  };

  // Connect: the extension opens the store's sign-in and returns the credential; it is tested and,
  // when it works, saved. A failing test keeps the value in the field, with the error.
  const connectField = async (f: SettingField, recipe: Recipe) => {
    const run = connect(type.id, f.key, recipe);
    running.current = run.cancel;
    setConnecting({ key: f.key, cancel: run.cancel });
    setResult(null);
    try {
      const value = await run.result;
      if (!mounted.current) return;
      const next = { ...settings, [f.key]: value };
      setSettings(next);
      if (await test(next) && mounted.current) await saveWith(next);
    } catch (e) {
      if (!mounted.current) return;
      const code = e instanceof ConnectorError ? e.code : 'failed';
      setResult({ tone: 'error', text: t(`connector.error.${code}`, { defaultValue: errorMessage(e) }) });
    } finally {
      running.current = null;
      if (mounted.current) setConnecting(null);
    }
  };

  const remove = async () => {
    if (!source || !confirm(t('sources.confirmDelete', { name: source.name }))) return;
    const deleteCopies = source.copyCount > 0 && confirm(t('sources.confirmDeleteCopies', { count: source.copyCount }));
    await action.run(async () => {
      await sourceClient.deleteSource({ id: source.id, deleteCopies });
      onDeleted();
    });
  };

  // Under a field the store signs in for: the sign-in page, and Connect when the extension can run
  // its recipe.
  const signInRow = (f: SettingField) => {
    const recipe = recipeOf(f);
    return (
      <>
        {(f.helpUrl || recipe) && (
          <div className="connector-row">
            <a className="button small-button" href={recipe?.open || f.helpUrl} target="_blank" rel="noreferrer">
              {t('connector.openSignIn')}<Icon name="external" size={14} />
            </a>
            {recipe && connector && recipe.version <= connector.recipeVersion && (
              connecting?.key === f.key
                ? <><span className="muted small">{t('connector.waiting')}</span><button type="button" className="small-button" onClick={connecting.cancel}>{t('common.cancel')}</button></>
                : <button type="button" className="small-button primary" disabled={locked || noConsent} onClick={() => connectField(f, recipe)}>{t('connector.connect')}</button>
            )}
            {recipe && connector && recipe.version > connector.recipeVersion && <span className="muted small">{t('connector.update')}</span>}
            {recipe && !connector && <span className="muted small">{t('connector.install')} <a href="https://github.com/botchris/game-vault/tree/main/extension" target="_blank" rel="noreferrer">{t('connector.howTo')}</a></span>}
          </div>
        )}
        {recipe?.private && connector && <p className="muted small">{t(connector.privateAllowed ? 'connector.privateOn' : 'connector.privateOff')}</p>}
      </>
    );
  };

  const r = source?.lastSync;

  // Items brought back in this dialog leave the list at once; the sources reload behind.
  const [restored, setRestored] = useState<string[]>([]);
  const { reloadSources } = useAppData();
  const removed = (source?.exclusions ?? []).filter((e) => !restored.includes(e.externalId));
  const includeAgain = async (externalId: string) => {
    try {
      await sourceClient.includeCopy({ sourceId: source!.id, externalId });
      setRestored((list) => [...list, externalId]);
      void reloadSources();
    } catch (e) {
      setResult({ tone: 'error', text: errorMessage(e) });
    }
  };

  return (
    <Modal title={source ? t('sources.editTitle', { name: source.name }) : t('sources.addTitle', { type: type.name })} onClose={onClose}
      footer={<>
        {source && <button type="button" className="danger" onClick={remove} disabled={locked}>{t('common.delete')}</button>}
        <TestButton testing={testing} disabled={locked || noConsent} onClick={() => test(settings)} />
        <span className="spacer" />
        <button type="button" onClick={onClose}>{t('common.cancel')}</button>
        {!source && <button type="button" onClick={(e) => save(e, true)} disabled={locked || noConsent}>{t('sources.saveAndSync')}</button>}
        <button type="submit" form="source-form" className="primary" disabled={locked || noConsent}>{t('common.save')}</button>
      </>}>
      <form id="source-form" onSubmit={(e) => save(e)}>
        <ConsentNotices fields={type.fields} settings={settings} onChange={setSettings} />
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
        {removed.length > 0 && (
          <section className="source-excluded">
            <h3>{t('sources.excludedTitle', { count: removed.length })}</h3>
            <p className="muted small">{t('sources.excludedHint')}</p>
            <ul>
              {removed.map((e) => (
                <li key={e.externalId}>
                  <span className="source-excluded-title">{e.title || e.externalId}</span>
                  <span className="muted small">{fmt.date(toDate(e.at))}</span>
                  <button type="button" className="small-button" disabled={locked} onClick={() => includeAgain(e.externalId)}>{t('sources.importAgain')}</button>
                </li>
              ))}
            </ul>
          </section>
        )}
        <div className="grid">
          <label className="span2">
            {t('sources.name')}
            <input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          {/* The sign-in link is the button of the connector row, not repeated in the help. */}
          <SettingInputs fields={type.fields} settings={settings} onChange={setSettings} idPrefix="source" helpLinks={false}
            renderExtra={signInRow} />
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
        <SettingsResultAlert result={result} />
      </form>
    </Modal>
  );
}
