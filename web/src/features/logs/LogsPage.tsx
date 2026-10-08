import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, logClient } from '../../api/client';
import { Alert, Modal, useFormatters } from '../../components/ui';
import type { LogFile, LogSettings } from '../../gen/gamevault/v1/log_pb';
import { downloadBytes, toDate } from '../../lib/model';

const LEVELS = ['debug', 'info', 'warn', 'error'] as const;

/** Log files in config/logs, their viewer, and the rotation settings (like Radarr/Sonarr's System → Logs). */
export default function LogsPage() {
  const { t } = useTranslation();
  const fmt = useFormatters();
  const [files, setFiles] = useState<LogFile[]>([]);
  const [viewing, setViewing] = useState<string | null>(null);
  const [notice, setNotice] = useState<{ tone: 'ok' | 'error'; text: string } | null>(null);

  const loadFiles = useCallback(async () => {
    try {
      setFiles((await logClient.listLogFiles({})).files);
    } catch (e) {
      setNotice({ tone: 'error', text: errorMessage(e) });
    }
  }, []);

  useEffect(() => {
    loadFiles();
  }, [loadFiles]);

  const download = async (name: string) => {
    try {
      const res = await logClient.getLogFile({ name });
      downloadBytes(name, new TextEncoder().encode(res.content), 'text/plain');
    } catch (e) {
      setNotice({ tone: 'error', text: errorMessage(e) });
    }
  };

  const clear = async () => {
    if (!confirm(t('logs.confirmClear'))) return;
    try {
      const res = await logClient.clearLogFiles({});
      setNotice({ tone: 'ok', text: t('logs.cleared', { count: res.deleted }) });
      await loadFiles();
    } catch (e) {
      setNotice({ tone: 'error', text: errorMessage(e) });
    }
  };

  const archived = files.filter((f) => !f.current).length;

  return (
    <div className="page">
      {notice && <Alert tone={notice.tone}>{notice.text}</Alert>}

      <LogSettingsCard onSaved={(text) => { setNotice({ tone: 'ok', text }); loadFiles(); }} onError={(text) => setNotice({ tone: 'error', text })} />

      <section className="card">
        <div className="section-head">
          <h2>{t('logs.files')}</h2>
          <div className="actions">
            <button onClick={loadFiles}>{t('logs.refresh')}</button>
            <button className="danger" onClick={clear} disabled={archived === 0}>{t('logs.clear', { count: archived })}</button>
          </div>
        </div>
        <div className="table-wrap">
          <table>
            <thead>
              <tr><th>{t('logs.file')}</th><th>{t('logs.modified')}</th><th>{t('system.backupSize')}</th><th /></tr>
            </thead>
            <tbody>
              {files.map((f) => (
                <tr key={f.name} onClick={() => setViewing(f.name)}>
                  <td><code>{f.name}</code>{f.current && <span className="badge">{t('logs.current')}</span>}</td>
                  <td>{fmt.dateTime(toDate(f.modifiedAt))}</td>
                  <td>{fmt.bytes(f.sizeBytes)}</td>
                  <td className="nowrap row-actions" onClick={(e) => e.stopPropagation()}>
                    <button onClick={() => setViewing(f.name)}>{t('logs.view')}</button>
                    <button onClick={() => download(f.name)}>{t('logs.download')}</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      {viewing && <LogViewer name={viewing} live={files.find((f) => f.name === viewing)?.current ?? false} onClose={() => setViewing(null)} onDownload={() => download(viewing)} />}
    </div>
  );
}

function LogSettingsCard({ onSaved, onError }: { onSaved: (msg: string) => void; onError: (msg: string) => void }) {
  const { t } = useTranslation();
  const [s, setS] = useState<Pick<LogSettings, 'level' | 'maxFileSizeMb' | 'maxFiles'> | null>(null);
  const [busy, setBusy] = useState(false);

  // Load once; onError is a fresh callback on every parent render, so it must not be a dependency.
  const [loadError, setLoadError] = useState('');
  useEffect(() => {
    logClient.getLogSettings({}).then((r) => setS(r.settings!)).catch((e) => setLoadError(errorMessage(e)));
  }, []);

  if (loadError) return <Alert tone="error">{loadError}</Alert>;

  if (!s) return null;

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const res = await logClient.updateLogSettings({ settings: s });
      setS(res.settings!);
      onSaved(t('logs.settingsSaved'));
    } catch (err) {
      onError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="card">
      <h2>{t('logs.settings')}</h2>
      <p className="muted">{t('logs.settingsIntro')}</p>
      <form className="grid grid-3" onSubmit={save}>
        <label>
          {t('logs.level')}
          <select value={s.level} onChange={(e) => setS({ ...s, level: e.target.value })}>
            {LEVELS.map((l) => <option key={l} value={l}>{t(`logs.levels.${l}`)}</option>)}
          </select>
        </label>
        <label>
          {t('logs.maxFileSize')}
          <input type="number" min={1} max={100} required value={s.maxFileSizeMb} onChange={(e) => setS({ ...s, maxFileSizeMb: Number(e.target.value) })} />
        </label>
        <label>
          {t('logs.maxFiles')}
          <input type="number" min={1} max={50} required value={s.maxFiles} onChange={(e) => setS({ ...s, maxFiles: Number(e.target.value) })} />
        </label>
        <p className="muted small span-all">{t('logs.maxTotal', { mb: s.maxFileSizeMb * s.maxFiles })}</p>
        <div className="actions span-all">
          <span className="spacer" />
          <button type="submit" className="primary" disabled={busy}>{t('common.save')}</button>
        </div>
      </form>
    </section>
  );
}

const TAILS = [500, 2000, 0] as const;

function LogViewer({ name, live, onClose, onDownload }: { name: string; live: boolean; onClose: () => void; onDownload: () => void }) {
  const { t } = useTranslation();
  const [tail, setTail] = useState<number>(500);
  const [filter, setFilter] = useState('');
  const [minLevel, setMinLevel] = useState<(typeof LEVELS)[number]>('debug');
  const [follow, setFollow] = useState(live);
  const [content, setContent] = useState('');
  const [truncated, setTruncated] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      const res = await logClient.getLogFile({ name, tailLines: tail });
      setContent(res.content);
      setTruncated(res.truncated);
      setError('');
    } catch (e) {
      setError(errorMessage(e));
    }
  }, [name, tail]);

  useEffect(() => {
    load();
    if (!follow) return;
    const timer = setInterval(load, 3000);
    return () => clearInterval(timer);
  }, [load, follow]);

  const lines = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    const minRank = LEVELS.indexOf(minLevel);
    return content
      .split('\n')
      .filter(Boolean)
      .map((text) => ({ text, level: levelOf(text) }))
      .filter((l) => (l.level === null || LEVELS.indexOf(l.level) >= minRank) && (!needle || l.text.toLowerCase().includes(needle)))
      .reverse(); // newest first, like Sonarr
  }, [content, filter, minLevel]);

  return (
    <Modal wide title={name} onClose={onClose}
      footer={<>
        <span className="muted small">{t('logs.lineCount', { count: lines.length })}{truncated && ` · ${t('logs.truncated')}`}</span>
        <span className="spacer" />
        <button onClick={onDownload}>{t('logs.download')}</button>
        <button onClick={onClose}>{t('common.close')}</button>
      </>}>
      <div className="toolbar">
        <input className="search" type="search" placeholder={t('logs.filter')} value={filter} onChange={(e) => setFilter(e.target.value)} />
        <select value={minLevel} onChange={(e) => setMinLevel(e.target.value as (typeof LEVELS)[number])} aria-label={t('logs.level')}>
          {LEVELS.map((l) => <option key={l} value={l}>{t('logs.minLevel', { level: t(`logs.levels.${l}`) })}</option>)}
        </select>
        <select value={tail} onChange={(e) => setTail(Number(e.target.value))} aria-label={t('logs.tail')}>
          {TAILS.map((n) => <option key={n} value={n}>{n ? t('logs.lastLines', { count: n }) : t('logs.wholeFile')}</option>)}
        </select>
        {live && (
          <label className="check-inline">
            <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> {t('logs.follow')}
          </label>
        )}
        <button onClick={load}>{t('logs.refresh')}</button>
      </div>
      {error && <Alert tone="error">{error}</Alert>}
      <pre className="logview">
        {lines.map((l, i) => <div key={i} className={`logline lvl-${l.level ?? 'none'}`}>{l.text}</div>)}
      </pre>
    </Modal>
  );
}

/** Extracts the level of a slog text line ("... level=WARN msg=..."). */
function levelOf(line: string): (typeof LEVELS)[number] | null {
  const m = / level=(DEBUG|INFO|WARN|ERROR)/.exec(line);
  return m ? (m[1]!.toLowerCase() as (typeof LEVELS)[number]) : null;
}
