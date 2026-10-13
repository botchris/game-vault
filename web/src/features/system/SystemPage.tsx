import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, systemClient, valuationClient } from '../../api/client';
import { Alert, useFormatters } from '../../components/ui';
import type { Backup, GetStatusResponse } from '../../gen/gamevault/v1/system_pb';
import type { SyncReport } from '../../gen/gamevault/v1/source_pb';
import type { GetCollectionValueResponse } from '../../gen/gamevault/v1/valuation_pb';
import { currencyList, formatAmount, regionCurrency } from '../../lib/money';
import { downloadBytes, toDate } from '../../lib/model';
import { hashTarget, issueHref, issueTitle, reasonText, sinceText } from '../../lib/health';
import { useHealth } from '../../state/Health';
import { useAppData } from '../../state/AppData';
import SecurityCard from './SecurityCard';
import { useAction } from '../../lib/useAction';

const CSV_TEMPLATE =
  'title;platform;kind;status;edition;grade;contents;location;acquiredOn;price;currency;origin;notes\n' +
  'Halo 3;Xbox 360;physical;owned;;very_good;box manual media;Living room shelf;2007-09-26;29.95;EUR;GAME;\n' +
  'The Last of Us;PS3;physical;owned;GOTY;good;box media;Box 2;;;;;\n' +
  'Diablo IV;Battle.net;library;owned;;;;;;;;;\n' +
  'Escape from Tarkov;Battlestate (Tarkov);library;owned;Edge of Darkness;;;;;;;;\n' +
  "Assassin's Creed Unity;Ubisoft Connect;library;owned;;;;;;;;;\n";

/** Scrolls to the card a "#/system/<id>" address names. */
function scrollToTarget(hash: string) {
  const id = hashTarget(hash);
  if (id) document.getElementById(id)?.scrollIntoView({ block: 'start' });
}

/** Follows a link to this page's own cards by scrolling: a click on the address already shown
 *  changes no hash, so it would do nothing. Links to other pages navigate as usual. */
function followOnPage(e: { preventDefault: () => void }, href: string) {
  if (!href.startsWith('#/system/')) return;
  e.preventDefault();
  window.history.replaceState(null, '', href);
  scrollToTarget(href);
}

/** Status, backups of the /config data and CSV import/export. */
export default function SystemPage() {
  const { t, i18n } = useTranslation();
  const fmt = useFormatters();
  const { reloadGames } = useAppData();
  const region = regionCurrency(i18n.language);
  const [currency, setCurrency] = useState(region);
  // Until the user saves one, the region's currency is only proposed: CSV prices without a
  // currency are not imported, so the proposal must be easy to save as it is.
  const [currencySaved, setCurrencySaved] = useState(true);
  const [status, setStatus] = useState<GetStatusResponse | null>(null);
  const [backups, setBackups] = useState<Backup[]>([]);
  const [photoStore, setPhotoStore] = useState(0n);
  const [value, setValue] = useState<GetCollectionValueResponse | null>(null);
  const { issues, unavailable, reload: reloadHealth } = useHealth();
  const { busy, error, setError, run } = useAction();
  // Which action runs, for its button's "Working…".
  const [doing, setDoing] = useState('');
  const [notice, setNotice] = useState<{ text: string; report?: SyncReport } | null>(null);

  const load = useCallback(async () => {
    const [st, bk, prefs, val] = await Promise.all([systemClient.getStatus({}), systemClient.listBackups({}), systemClient.getPreferences({}),
      valuationClient.getCollectionValue({}).catch((e) => {
        setError(errorMessage(e)); // the rest of the page still works
        return null;
      })]);
    setValue(val);
    setStatus(st);
    setBackups(bk.backups);
    setPhotoStore(bk.photoStoreBytes);
    if (prefs.preferences?.currency) setCurrency(prefs.preferences.currency);
    setCurrencySaved(!!prefs.preferences?.currency);
  }, [setError]);

  useEffect(() => {
    load().catch((e) => setError(errorMessage(e)));
  }, [load, setError]);

  // "#/system/backups" (a health issue's link) scrolls to that card, now and on later clicks.
  useEffect(() => {
    const scroll = () => scrollToTarget(window.location.hash);
    const timer = window.setTimeout(scroll, 0); // after the page's first render
    window.addEventListener('hashchange', scroll);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener('hashchange', scroll);
    };
  }, []);

  const act = (name: string, fn: () => Promise<void>) => {
    setDoing(name);
    setNotice(null);
    return run(fn);
  };

  const saveCurrency = (c: string) => act('currency', async () => {
    const res = await systemClient.updatePreferences({ preferences: { currency: c } });
    setCurrency(res.preferences?.currency || c);
    setCurrencySaved(true);
  });

  const importCsv = (file: File) => act('import', async () => {
    const res = await systemClient.importCsv({ content: new Uint8Array(await file.arrayBuffer()) });
    const r = res.report!;
    setNotice({ report: r, text: t('system.importDone', { added: r.copiesAdded, updated: r.copiesUpdated, unchanged: r.copiesUnchanged, games: r.gamesCreated }) });
    await Promise.all([reloadGames(), load()]);
  });

  return (
    <div className="page">
      {error && <Alert tone="error">{error}</Alert>}
      {notice && (
        <Alert tone="ok">
          {notice.text}
          {notice.report && notice.report.warnings.length > 0 && (
            <details><summary>{t('sources.warnings', { count: notice.report.warnings.length })}</summary>
              <ul>{notice.report.warnings.map((w, i) => <li key={i}>{w}</li>)}</ul></details>
          )}
        </Alert>
      )}

      <section className="card" id="health">
        <h2>{t('health.title')}</h2>
        {unavailable && <p className="muted small">{t('health.unavailable')}</p>}
        {issues.length === 0 ? !unavailable && <p className="health-ok">{t('health.none')}</p> : (
          <>
            <p className="muted small">{t('health.intro')}</p>
            <ul className="health-list">
              {issues.map((issue) => {
                const href = issueHref(issue);
                const since = toDate(issue.since);
                return (
                  <li key={issue.key} className={`health-issue ${issue.level}`}>
                    <span className={`health-dot ${issue.level}`} role="img" aria-label={t(`health.${issue.level === 'error' ? 'error' : 'warning'}`)} />
                    <div className="health-body">
                      <strong>{issueTitle(issue, t)}</strong>
                      <span>{reasonText(issue, t, new Date(), i18n.language)}</span>
                      {issue.message && <span className="muted small health-detail">{issue.message}</span>}
                      <span className="muted small">
                        {since && <time dateTime={since.toISOString()} title={fmt.dateTime(since)}>{sinceText(since, new Date(), i18n.language)}</time>}
                        {since && href && ' · '}
                        {href && <a href={href} onClick={(e) => followOnPage(e, href)}>{t('health.open')}</a>}
                      </span>
                    </div>
                  </li>
                );
              })}
            </ul>
          </>
        )}
      </section>

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
          <span className="row tight">
            <select value={currency} disabled={busy} onChange={(e) => saveCurrency(e.target.value)}>
              {[region, ...currencyList().filter((c) => c !== region)].map((c) => <option key={c} value={c}>{c}</option>)}
            </select>
            {!currencySaved && (
              <button type="button" className="primary" disabled={busy} onClick={() => saveCurrency(currency)}>{t('common.save')}</button>
            )}
          </span>
          <span className="help">{currencySaved ? t('system.currencyHelp') : t('system.currencyNotSaved')}</span>
        </label>
      </section>

      <SecurityCard />

      {/* Only sources with something to add up: before the first estimates there is nothing to show. */}
      {value && value.totals.some((v) => v.copies > 0 || v.otherCurrency > 0) && (
        <section className="card">
          <h2>{t('system.collectionValue')}</h2>
          {!value.currency && <p className="muted">{t('system.valueNoCurrency')}</p>}
          {value.currency && value.totals.filter((v) => v.copies > 0 || v.otherCurrency > 0).map((v) => {
            const money = (minor: bigint) => formatAmount(minor, value.currency, i18n.language);
            const other = v.otherCurrency > 0 && t('system.valueOther', { count: v.otherCurrency });
            if (v.copies === 0) return <p key={v.provider} className="muted">{v.name}: {other}</p>;
            return (
              <p key={v.provider}>
                {v.buyCashMinor > 0n || v.buyCreditMinor > 0n
                  ? t('system.valueShop', { name: v.name, sell: money(v.sellMinor), cash: money(v.buyCashMinor), credit: money(v.buyCreditMinor) })
                  : t('system.valueListed', { name: v.name, sell: money(v.sellMinor) })}
                {' · '}<span className="muted">{t('system.valueCopies', { count: v.copies })}</span>
                {other && <span className="muted"> · {other}</span>}
              </p>
            );
          })}
        </section>
      )}

      <section className="card" id="backups">
        <div className="section-head">
          <h2>{t('system.backups')}</h2>
          <button className="primary" disabled={busy} onClick={() => act('backup', async () => {
            const res = await systemClient.createBackup({});
            reloadHealth(); // a backup may clear a failed one
            setNotice({ text: t('system.backupCreated', { name: res.backup?.name }) });
            await load();
          })}>{busy && doing === 'backup' ? t('common.working') : t('system.backupNow')}</button>
        </div>
        {backups.length === 0 ? <p className="muted">{t('system.noBackups')}</p> : (
          <table>
            <thead><tr><th>{t('system.backupName')}</th><th>{t('system.backupDate')}</th><th>{t('system.backupSize')}</th><th>{t('system.backupPhotos')}</th></tr></thead>
            <tbody>
              {backups.map((b) => (
                <tr key={b.name} className="static"><td><code>{b.name}</code></td><td>{fmt.dateTime(toDate(b.createdAt))}</td><td>{fmt.bytes(b.sizeBytes)}</td><td>{b.photoCount}</td></tr>
              ))}
            </tbody>
          </table>
        )}
        {photoStore > 0n && <p className="muted small">{t('system.photoStore', { size: fmt.bytes(photoStore) })}</p>}
        <p className="muted small">{t('system.backupsHint')}</p>
      </section>

      <section className="card">
        <h2>{t('system.csv')}</h2>
        <p className="muted">{t('system.csvIntro')}</p>
        <div className="row">
          <label className={`button primary file ${busy ? 'disabled' : ''}`}>
            {busy && doing === 'import' ? t('common.working') : t('system.importCsv')}
            <input type="file" accept=".csv,text/csv" hidden disabled={busy}
              onChange={(e) => { const f = e.target.files?.[0]; e.target.value = ''; if (f) importCsv(f); }} />
          </label>
          <button onClick={() => downloadBytes('gamevault-template.csv', new TextEncoder().encode(CSV_TEMPLATE), 'text/csv')}>{t('system.downloadTemplate')}</button>
          <button disabled={busy} onClick={() => act('export', async () => {
            const res = await systemClient.exportCsv({});
            downloadBytes(res.filename, res.content, 'text/csv');
          })}>{t('system.exportCsv')}</button>
        </div>
        <p className="muted small">{t('system.csvColumns')}</p>
      </section>
    </div>
  );
}
