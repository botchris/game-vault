import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../../components/PageHeader';
import { errorMessage, gameClient, lookupClient } from '../../api/client';
import { Cover } from '../../components/Cover';
import { Icon } from '../../components/Icon';
import SystemPicker from '../../components/SystemPicker';
import { Alert } from '../../components/ui';
import type { IdentifyBarcodeResponse } from '../../gen/gamevault/v1/lookup_pb';
import { CopyContent, CopyGrade, CopyRegion } from '../../gen/gamevault/v1/game_pb';
import { CopyKind, CopyStatus, describePhysical, emptyDetails, regionKey } from '../../lib/model';
import { normalizeBarcode } from '../../lib/barcode.ts';
import { recall, remember } from '../../lib/storage';
import { useAppData } from '../../state/AppData';
import GameDetail from '../library/GameDetail';
import { ContentsToggles, GradeSelect, LocationInput, RegionSelect } from '../library/PhysicalInputs';
import { signal, unlockAudio } from './feedback';
import { createLookupQueue } from './lookupQueue';
import { ScanRow } from './ScanRow';
import {
  addCode, amend, applyResults, chunkItems, loadRows, plusOne, remove, restore, retry, saveRows, sendItems, settle, summary, type Answer, type Row,
} from './scanList.ts';

const CameraScanner = lazy(() => import('./CameraScanner'));

const DEFAULTS_KEY = 'gamevault.scanDefaults';
const CAMERA_KEY = 'gamevault.scanCamera';

interface Defaults {
  platform: string;
  grade: CopyGrade;
  /** Applied to every box when set; otherwise each box takes the region its lookup found (see sendItems). */
  region: CopyRegion;
  contents: CopyContent[];
  location: string;
}

const NO_DEFAULTS: Defaults = { platform: '', grade: CopyGrade.UNSPECIFIED, region: CopyRegion.UNSPECIFIED, contents: [], location: '' };

function loadDefaults(): Defaults {
  try {
    return { ...NO_DEFAULTS, ...JSON.parse(recall(DEFAULTS_KEY) ?? '{}') };
  } catch {
    return NO_DEFAULTS; // saved by hand or damaged: not JSON
  }
}

/** The camera starts open on phones (where it is the scanner) unless it was closed last time. */
function initialCamera(): boolean {
  const saved = recall(CAMERA_KEY);
  if (saved !== null) return saved === '1';
  return window.isSecureContext && window.matchMedia('(pointer: coarse)').matches;
}

const LIST_KEY = 'gamevault.scanList';
const MUTED_KEY = 'gamevault.scanMuted';
const SEND_CHUNK = 200;

/** A row id: unique on this device (crypto.randomUUID needs HTTPS, which a LAN address may lack). */
const newId = () => `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;

/** The plain parts of an IdentifyBarcode answer a row keeps. */
function toAnswer(res: IdentifyBarcodeResponse): Answer {
  return {
    barcode: res.barcode,
    owned: res.owned.map((o) => ({ gameId: o.game?.id ?? '', title: o.game?.title ?? '', platform: o.platform })),
    match: res.match ? { raw: res.match.raw, title: res.match.title, platform: res.match.platform, edition: res.match.edition, providerId: res.match.providerId, region: res.match.region } : null,
    suggestions: res.suggestions.map((s) => ({ title: s.title, platform: s.platform, coverUrl: s.coverUrl, thumbUrl: s.thumbUrl, label: s.label })),
    existing: res.existing.map((g) => ({ id: g.id, title: g.title })),
    warnings: res.warnings,
    providersUnavailable: res.providersUnavailable,
  };
}

interface Added {
  gameId: string;
  key: string;
}

/**
 * Registering a shelf of physical games: the camera (or a reader, or typing) keeps reading, every
 * box joins a list kept on this device and looked up one at a time in the background, and Send
 * adds the ready ones at once. Nothing asks for a confirmation while scanning.
 */
export default function ScanPage() {
  const { t } = useTranslation();
  const { games, putGame } = useAppData();
  const [openGame, setOpenGame] = useState<string | null>(null);
  const [openRow, setOpenRow] = useState<string | null>(null);
  const [code, setCode] = useState('');
  const [camera, setCamera] = useState(initialCamera);
  const [muted, setMuted] = useState(() => recall(MUTED_KEY) === '1');
  const [defaults, setDefaults] = useState<Defaults>(loadDefaults);
  const [rows, setRows] = useState<Row[]>(() => loadRows(recall(LIST_KEY)));
  const rowsRef = useRef(rows);
  const [notice, setNotice] = useState<{ tone: 'warn' | 'error'; text: string } | null>(null);
  const [sending, setSending] = useState(false);
  const [added, setAdded] = useState<Added[]>([]);
  const [undo, setUndo] = useState<{ text: string; apply: () => void } | null>(null);
  const warnedStorage = useRef(false);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => { remember(DEFAULTS_KEY, JSON.stringify(defaults)); }, [defaults]);

  // Every change goes through here: the ref lets handlers read the list synchronously (a read
  // and a lookup answer can arrive in the same tick), and the list is saved on the device.
  // t changes with the language; through a ref, update (and the lookup queue made from it) stay
  // the same, so a language change does not start a second queue.
  const tRef = useRef(t);
  tRef.current = t;
  const update = useCallback((fn: (r: Row[]) => Row[]) => {
    const next = fn(rowsRef.current);
    if (next === rowsRef.current) return;
    rowsRef.current = next;
    setRows(next);
    if (!remember(LIST_KEY, saveRows(next)) && !warnedStorage.current) {
      warnedStorage.current = true;
      setNotice({ tone: 'warn', text: tRef.current('scan.list.noStorage') });
    }
  }, []);

  const queue = useMemo(() => createLookupQueue(
    (c: string) => lookupClient.identifyBarcode({ barcode: c }).then(toAnswer),
    (id, answer, error) => update((r) => settle(r, id, answer, error === undefined ? undefined : errorMessage(error))),
  ), [update]);

  // Rows still looking up when the page was closed are looked up again.
  useEffect(() => {
    for (const r of rowsRef.current) if (r.phase === 'looking') queue.push(r.id, r.code);
  }, [queue]);

  // Browsers let a page play sound only after a tap or a key: the first one anywhere unlocks the
  // beeps (on phones the camera opens by itself, so no button tap may come before the first read).
  useEffect(() => {
    const unlock = () => unlockAudio();
    window.addEventListener('pointerdown', unlock, { once: true });
    window.addEventListener('keydown', unlock, { once: true });
    return () => {
      window.removeEventListener('pointerdown', unlock);
      window.removeEventListener('keydown', unlock);
    };
  }, []);

  // Undo offers last a few seconds.
  useEffect(() => {
    if (!undo) return;
    const timer = window.setTimeout(() => setUndo(null), 6000);
    return () => window.clearTimeout(timer);
  }, [undo]);

  /** A read from the camera, a reader or the keyboard. */
  const take = (raw: string): 'added' | 'repeat' | 'invalid' => {
    const res = addCode(rowsRef.current, raw, newId());
    if (res.outcome === 'invalid') return 'invalid';
    if (res.outcome === 'added') {
      update(() => res.rows);
      queue.push(res.row!.id, res.row!.code);
    }
    signal(res.outcome === 'added' ? 'new' : 'repeat', muted);
    return res.outcome;
  };

  const submitCode = (e: FormEvent) => {
    e.preventDefault();
    const raw = code.trim();
    if (!raw) return;
    const outcome = take(raw);
    if (outcome === 'invalid') {
      setNotice({ tone: 'error', text: t('scan.invalid', { code: raw }) });
      return;
    }
    setNotice(outcome === 'repeat' ? { tone: 'warn', text: t('scan.list.repeat', { code: normalizeBarcode(raw) }) } : null);
    setCode('');
    input.current?.focus();
  };

  const toggleCamera = () => {
    unlockAudio();
    setCamera(!camera);
    remember(CAMERA_KEY, camera ? '0' : '1');
  };

  const toggleMute = () => {
    unlockAudio();
    setMuted(!muted);
    remember(MUTED_KEY, muted ? '0' : '1');
  };

  const removeRow = (id: string) => {
    const { rows: next, removed } = remove(rowsRef.current, id);
    if (!removed) return;
    queue.drop(id);
    update(() => next);
    setUndo({ text: t('scan.list.removed', { code: removed.row.code }), apply: () => {
      update((r) => restore(r, removed));
      if (removed.row.phase === 'looking') queue.push(removed.row.id, removed.row.code);
    } });
  };

  const clearList = () => {
    const before = rowsRef.current;
    before.forEach((r) => queue.drop(r.id));
    update(() => []);
    setOpenRow(null);
    setUndo({ text: t('scan.list.cleared'), apply: () => {
      update(() => before);
      before.filter((r) => r.phase === 'looking').forEach((r) => queue.push(r.id, r.code));
    } });
  };

  const send = async () => {
    const items = sendItems(rowsRef.current, defaults.platform, defaults.region);
    if (!items.length) return;
    setSending(true);
    setNotice(null);
    try {
      for (const chunk of chunkItems(items, SEND_CHUNK)) {
        const res = await gameClient.addScannedCopies({
          items: chunk.map((it) => ({
            clientId: it.clientId, gameId: it.gameId, title: it.title, coverUrl: it.coverUrl,
            details: {
              ...emptyDetails(CopyKind.PHYSICAL), status: CopyStatus.OWNED, platform: it.platform, edition: it.edition,
              grade: defaults.grade, region: it.region, contents: defaults.contents, location: defaults.location, barcode: it.barcode,
            },
          })),
        });
        res.games.forEach(putGame);
        const { rows: next, saved } = applyResults(rowsRef.current, res.results);
        update(() => next);
        setAdded((list) => [...saved.map((s) => ({ gameId: s.gameId, key: `${s.row.id}-${s.gameId}` })), ...list].slice(0, 60));
      }
      setOpenRow(null);
    } catch (e) {
      setNotice({ tone: 'error', text: t('scan.list.sendFailed', { error: errorMessage(e) }) });
    } finally {
      setSending(false);
    }
  };

  const sum = summary(rows, defaults.platform);

  return (
    <div className="page scan">
      <PageHeader title={t('scan.title')} />
      <p className="muted scan-intro">{t('scan.intro')}</p>

      <section className="card scanner">
        {camera && (
          <Suspense fallback={<div className="camera camera-loading" />}>
            <CameraScanner onCode={take} />
          </Suspense>
        )}
        <form className="scanner-form" onSubmit={submitCode}>
          <input ref={input} className="barcode-input" inputMode="numeric" autoFocus={!camera} autoComplete="off"
            placeholder={t(camera ? 'scan.placeholderCamera' : 'scan.placeholder')} aria-label={t('scan.placeholder')}
            value={code} onChange={(e) => setCode(e.target.value)} />
          <button type="submit" className="primary" disabled={!code.trim()}>{t('scan.lookup')}</button>
          <button type="button" className={`camera-toggle ${camera ? 'active' : ''}`} onClick={toggleCamera}
            aria-pressed={camera} title={t(camera ? 'scan.camera.stop' : 'scan.camera.start')}>
            <Icon name="camera" size={18} />
            <span className="camera-toggle-label">{t(camera ? 'scan.camera.stop' : 'scan.camera.start')}</span>
          </button>
          {/* A toggle with a fixed name: pressed means the sound is on. */}
          <button type="button" className="camera-toggle" onClick={toggleMute} aria-pressed={!muted} title={t('scan.list.sound')}>
            <Icon name={muted ? 'muted' : 'sound'} size={18} />
            <span className="camera-toggle-label">{t('scan.list.sound')}</span>
          </button>
        </form>
        <BatchBar defaults={defaults} onChange={setDefaults} />
      </section>

      {notice && <Alert tone={notice.tone}>{notice.text}</Alert>}

      <section className="scan-list-section">
        <h2 className="scan-list-title">{t('scan.list.title', { count: rows.length })}</h2>
        {rows.length === 0 ? <p className="muted">{t('scan.list.empty')}</p> : (
          <>
            <ul className="scan-list">
              {rows.map((r) => (
                <ScanRow key={r.id} row={r} platform={defaults.platform} open={openRow === r.id}
                  onToggle={() => setOpenRow(openRow === r.id ? null : r.id)}
                  onPlus={() => update((x) => plusOne(x, r.id))}
                  onRemove={() => removeRow(r.id)} locked={sending}
                  onRetry={() => { update((x) => retry(x, r.id)); queue.push(r.id, r.code); }}
                  onAmend={(patch) => update((x) => amend(x, r.id, patch))} />
              ))}
            </ul>
            <button type="button" className="link scan-clear" onClick={clearList} disabled={sending}>{t('scan.list.clear')}</button>
          </>
        )}
      </section>

      {added.length > 0 && (
        <section className="scan-added">
          <h2 className="scan-added-title">{t('scan.list.added', { count: added.length })}</h2>
          <ul>
            {added.map((a) => {
              const g = games.find((x) => x.id === a.gameId);
              return (
                <li key={a.key}>
                  <button onClick={() => setOpenGame(a.gameId)} title={g?.title}>
                    {g ? <Cover game={g} /> : <span className="thumb-placeholder" />}
                    <span className="scan-added-name">{g?.title}</span>
                  </button>
                </li>
              );
            })}
          </ul>
        </section>
      )}

      {(rows.length > 0 || undo) && (
        <div className="scan-sendbar" role="region" aria-label={t('scan.list.title', { count: rows.length })}>
          {undo ? (
            <p className="scan-sendbar-undo">{undo.text} <button type="button" className="link" disabled={sending} onClick={() => { undo.apply(); setUndo(null); }}>{t('scan.list.undo')}</button></p>
          ) : (
            <p className="scan-sendbar-summary">
              {t('scan.list.summary', { ready: sum.ready, review: sum.review, owned: sum.owned })}
              {sum.looking > 0 && <> · <span className="spinner" aria-hidden="true" /> {t('scan.list.sendLooking', { count: sum.looking })}</>}
            </p>
          )}
          <button type="button" className="primary" disabled={sending || sum.copies === 0} onClick={send}>
            {sending ? t('scan.list.sending') : t('scan.list.send', { count: sum.copies })}
          </button>
        </div>
      )}

      {openGame && <GameDetail key={openGame} gameId={openGame} onClose={() => setOpenGame(null)} onOpenGame={setOpenGame} />}
    </div>
  );
}

/** What is applied to every box of the batch, always visible; the fields open in place. */
function BatchBar({ defaults, onChange }: { defaults: Defaults; onChange: (d: Defaults) => void }) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);
  const value = (v: string, empty: string) => v || empty;
  const physical = describePhysical(t, defaults);
  return (
    <div className="batch">
      <button type="button" className="batch-summary" onClick={() => setEditing(!editing)} aria-expanded={editing}>
        <span className="batch-label">{t('scan.batch.label')}</span>
        <span className="batch-chip">{t('scan.batch.platform')}: <strong>{value(defaults.platform, t('scan.batch.auto'))}</strong></span>
        <span className="batch-chip">{t('copy.grade')}: <strong>{value(physical.grade, '—')}</strong></span>
        <span className="batch-chip">{t('copy.region')}: <strong>{defaults.region ? t(`region.${regionKey(defaults.region)}`) : '—'}</strong></span>
        <span className="batch-chip">{t('copy.contents')}: <strong>{value(physical.contents, '—')}</strong></span>
        <span className="batch-chip">{t('copy.location')}: <strong>{value(defaults.location, '—')}</strong></span>
        <span className="batch-edit-label">{editing ? t('scan.batch.done') : t('scan.batch.change')}</span>
      </button>
      {editing && (
        <div className="batch-fields">
          {/* Focused when the user opens these fields; the list opens only when they type or click. */}
          <SystemPicker label={t('scan.batch.platform')} value={defaults.platform} autoFocus
            onChange={(platform) => onChange({ ...defaults, platform })} placeholder={t('scan.batch.platformAuto')} />
          <GradeSelect value={defaults.grade} onChange={(grade) => onChange({ ...defaults, grade })} />
          <RegionSelect value={defaults.region} onChange={(region) => onChange({ ...defaults, region })} />
          <ContentsToggles value={defaults.contents} onChange={(contents) => onChange({ ...defaults, contents })} />
          <LocationInput value={defaults.location} onChange={(location) => onChange({ ...defaults, location })} />
          <p className="muted small batch-hint">{t('scan.batch.hint')}</p>
        </div>
      )}
    </div>
  );
}
