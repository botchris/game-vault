import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, systemClient } from '../../api/client';
import { Alert, useFormatters } from '../../components/ui';
import type { Backup, GetStatusResponse } from '../../gen/gamevault/v1/system_pb';
import type { SyncReport } from '../../gen/gamevault/v1/source_pb';
import { currencyList, regionCurrency } from '../../lib/money';
import { downloadBytes, toDate } from '../../lib/model';
import { useAppData } from '../../state/AppData';
import SecurityCard from './SecurityCard';

const CSV_TEMPLATE =
  'title;platform;kind;status;edition;grade;contents;location;acquiredOn;price;currency;origin;notes\n' +
  'Halo 3;Xbox 360;physical;owned;;very_good;box manual media;Living room shelf;2007-09-26;29.95;EUR;GAME;\n' +
  'The Last of Us;PS3;physical;owned;GOTY;good;box media;Box 2;;;;;\n' +
  'Diablo IV;Battle.net;library;owned;;;;;;;;;\n' +
  'Escape from Tarkov;Battlestate (Tarkov);library;owned;Edge of Darkness;;;;;;;;\n' +
  "Assassin's Creed Unity;Ubisoft Connect;library;owned;;;;;;;;;\n";

/** Status, backups of the /config data and CSV import/export. */
export default function SystemPage() {
  const { t, i18n } = useTranslation();
  const fmt = useFormatters();
  const { reloadGames } = useAppData();
  const region = regionCurrency(i18n.language);
  const [currency, setCurrency] = useState(region);
  const [status, setStatus] = useState<GetStatusResponse | null>(null);
  const [backups, setBackups] = useState<Backup[]>([]);
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState<{ tone: 'ok' | 'error'; text: string; report?: SyncReport } | null>(null);

  const load = useCallback(async () => {
    const [st, bk, prefs] = await Promise.all([systemClient.getStatus({}), systemClient.listBackups({}), systemClient.getPreferences({})]);
    setStatus(st);
    setBackups(bk.backups);
    if (prefs.preferences?.currency) setCurrency(prefs.preferences.currency);
  }, []);

  useEffect(() => {
    load().catch((e) => setNotice({ tone: 'error', text: errorMessage(e) }));
  }, [load]);

  const run = async (name: string, fn: () => Promise<void>) => {
    setBusy(name);
    setNotice(null);
    try {
      await fn();
    } catch (e) {
      setNotice({ tone: 'error', text: errorMessage(e) });
    } finally {
      setBusy('');
    }
  };

  const importCsv = (file: File) => run('import', async () => {
    const res = await systemClient.importCsv({ content: new Uint8Array(await file.arrayBuffer()) });
    const r = res.report!;
    setNotice({ tone: 'ok', report: r, text: t('system.importDone', { added: r.copiesAdded, updated: r.copiesUpdated, unchanged: r.copiesUnchanged, games: r.gamesCreated }) });
    await Promise.all([reloadGames(), load()]);
  });

  return (
    <div className="page">
      {notice && (
        <Alert tone={notice.tone}>
          {notice.text}
          {notice.report && notice.report.warnings.length > 0 && (
            <details><summary>{t('sources.warnings', { count: notice.report.warnings.length })}</summary>
              <ul>{notice.report.warnings.map((w, i) => <li key={i}>{w}</li>)}</ul></details>
          )}
        </Alert>
      )}

      <section className="card">
        <h2>{t('system.status')}</h2>
        {status && (
          <dl className="kv">
            <dt>{t('system.version')}</dt><dd>{status.version}</dd>
            <dt>{t('system.configDir')}</dt><dd><code>{status.configDir}</code></dd>
            <dt>{t('system.database')}</dt><dd><code>{status.databasePath}</code></dd>
            <dt>{t('system.startedAt')}</dt><dd>{fmt.dateTime(toDate(status.startedAt))}</dd>
            <dt>{t('system.counts')}</dt><dd>{t('system.countsValue', { games: status.gameCount, copies: status.copyCount })}</dd>
          </dl>
        )}
        <p className="muted small">{t('system.configHint')}</p>
      </section>

      <section className="card">
        <h2>{t('system.preferences')}</h2>
        <label className="field">
          {t('system.currency')}
          <select value={currency} disabled={!!busy} onChange={(e) => {
            const c = e.target.value;
            run('currency', async () => {
              const res = await systemClient.updatePreferences({ preferences: { currency: c } });
              setCurrency(res.preferences?.currency || c);
            });
          }}>
            {[region, ...currencyList().filter((c) => c !== region)].map((c) => <option key={c} value={c}>{c}</option>)}
          </select>
          <span className="help">{t('system.currencyHelp')}</span>
        </label>
      </section>

      <SecurityCard />

      <section className="card">
        <div className="section-head">
          <h2>{t('system.backups')}</h2>
          <button className="primary" disabled={!!busy} onClick={() => run('backup', async () => {
            const res = await systemClient.createBackup({});
            setNotice({ tone: 'ok', text: t('system.backupCreated', { name: res.backup?.name }) });
            await load();
          })}>{busy === 'backup' ? t('common.working') : t('system.backupNow')}</button>
        </div>
        {backups.length === 0 ? <p className="muted">{t('system.noBackups')}</p> : (
          <table>
            <thead><tr><th>{t('system.backupName')}</th><th>{t('system.backupDate')}</th><th>{t('system.backupSize')}</th></tr></thead>
            <tbody>
              {backups.map((b) => (
                <tr key={b.name} className="static"><td><code>{b.name}</code></td><td>{fmt.dateTime(toDate(b.createdAt))}</td><td>{fmt.bytes(b.sizeBytes)}</td></tr>
              ))}
            </tbody>
          </table>
        )}
        <p className="muted small">{t('system.backupsHint')}</p>
      </section>

      <section className="card">
        <h2>{t('system.csv')}</h2>
        <p className="muted">{t('system.csvIntro')}</p>
        <div className="row">
          <label className={`button primary file ${busy ? 'disabled' : ''}`}>
            {busy === 'import' ? t('common.working') : t('system.importCsv')}
            <input type="file" accept=".csv,text/csv" hidden disabled={!!busy}
              onChange={(e) => { const f = e.target.files?.[0]; e.target.value = ''; if (f) importCsv(f); }} />
          </label>
          <button onClick={() => downloadBytes('gamevault-template.csv', new TextEncoder().encode(CSV_TEMPLATE), 'text/csv')}>{t('system.downloadTemplate')}</button>
          <button disabled={!!busy} onClick={() => run('export', async () => {
            const res = await systemClient.exportCsv({});
            downloadBytes(res.filename, res.content, 'text/csv');
          })}>{t('system.exportCsv')}</button>
        </div>
        <p className="muted small">{t('system.csvColumns')}</p>
      </section>
    </div>
  );
}
