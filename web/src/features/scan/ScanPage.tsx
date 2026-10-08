import { lazy, Suspense, useEffect, useRef, useState, type FormEvent, type KeyboardEvent, type MutableRefObject } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, gameClient, lookupClient, proxiedImage } from '../../api/client';
import { Cover } from '../../components/Cover';
import { Icon } from '../../components/Icon';
import { PlatformBadge } from '../../components/PlatformBadge';
import { Alert } from '../../components/ui';
import type { GameRef, GameSuggestion, IdentifyBarcodeResponse } from '../../gen/gamevault/v1/lookup_pb';
import { CopyKind, CopyStatus, PHYSICAL_PLATFORMS, emptyDetails, validBarcode, type Game } from '../../lib/model';
import { useAppData } from '../../state/AppData';
import GameDetail from '../library/GameDetail';

const CameraScanner = lazy(() => import('./CameraScanner'));

const DEFAULTS_KEY = 'gamevault.scanDefaults';
const CAMERA_KEY = 'gamevault.scanCamera';

/** Names of the barcode databases, for "found in …". */
const PROVIDER_NAMES: Record<string, string> = { cex: 'CeX', ebay: 'eBay', upcitemdb: 'UPCitemdb', eansearch: 'EAN-Search' };

interface Defaults {
  platform: string;
  condition: string;
  location: string;
}

function loadDefaults(): Defaults {
  try {
    return { platform: '', condition: '', location: '', ...JSON.parse(localStorage.getItem(DEFAULTS_KEY) ?? '{}') };
  } catch {
    return { platform: '', condition: '', location: '' };
  }
}

/** The camera starts open on phones (where it is the scanner) unless it was closed last time. */
function initialCamera(): boolean {
  try {
    const saved = localStorage.getItem(CAMERA_KEY);
    if (saved !== null) return saved === '1';
  } catch {
    /* fall through to the device default */
  }
  return window.isSecureContext && window.matchMedia('(pointer: coarse)').matches;
}

function remember(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* not remembered in private mode */
  }
}

interface Added {
  gameId: string;
  title: string;
  platform: string;
  thumb: string;
}

/**
 * Quick registration of physical games: scan (camera, USB/Bluetooth reader or typing) → confirm →
 * next. With a reader the whole loop is keyboard: a code and Enter look it up, Enter again adds
 * it, and a new code skips the pending one. Unknown barcodes are identified by title once and
 * remembered through the copy's barcode.
 */
export default function ScanPage() {
  const { t } = useTranslation();
  const { putGame } = useAppData();
  const [openId, setOpenId] = useState<string | null>(null);
  const [code, setCode] = useState('');
  const [camera, setCamera] = useState(initialCamera);
  const [busy, setBusy] = useState(false);
  // The code being looked up, shown at once (over the frozen camera and as a placeholder card):
  // lookups can take seconds while several barcode databases are asked in turn.
  const [searching, setSearching] = useState('');
  const [error, setError] = useState('');
  const [result, setResult] = useState<IdentifyBarcodeResponse | null>(null);
  const [defaults, setDefaults] = useState<Defaults>(loadDefaults);
  const [added, setAdded] = useState<Added[]>([]);
  const input = useRef<HTMLInputElement>(null);
  // The pending result's main action, so Enter in the barcode field can confirm it.
  const confirm = useRef<(() => void) | null>(null);

  useEffect(() => remember(DEFAULTS_KEY, JSON.stringify(defaults)), [defaults]);

  const toggleCamera = () => {
    setCamera(!camera);
    remember(CAMERA_KEY, camera ? '0' : '1');
  };

  const identify = async (raw: string) => {
    if (!validBarcode(raw)) {
      setError(t('scan.invalid', { code: raw }));
      return;
    }
    setBusy(true);
    setSearching(raw.replace(/\D/g, ''));
    setCode(raw);
    setError('');
    setResult(null);
    try {
      setResult(await lookupClient.identifyBarcode({ barcode: raw }));
      setCode('');
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
      setSearching('');
    }
  };

  const next = () => {
    setResult(null);
    setError('');
    input.current?.focus();
  };

  const submitCode = (e: FormEvent) => {
    e.preventDefault();
    if (code.trim()) identify(code);
  };

  // Enter in the empty field confirms the pending result. Browsers do not submit a form whose
  // submit button is disabled (as it is while the field is empty), so this is handled here.
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' && !code.trim() && confirm.current) {
      e.preventDefault();
      confirm.current();
    }
  };

  return (
    <div className="page scan">
      <header className="page-head">
        <h1 className="page-title">{t('scan.title')}</h1>
      </header>
      <p className="muted scan-intro">{t('scan.intro')}</p>

      <section className="card scanner">
        {camera && (
          <Suspense fallback={<div className="camera camera-loading" />}>
            <CameraScanner paused={busy || result !== null} searching={searching} onCode={identify} />
          </Suspense>
        )}
        <form className="scanner-form" onSubmit={submitCode}>
          <input ref={input} className="barcode-input" inputMode="numeric" autoFocus={!camera} autoComplete="off"
            placeholder={t(camera ? 'scan.placeholderCamera' : 'scan.placeholder')} aria-label={t('scan.placeholder')}
            value={code} onChange={(e) => setCode(e.target.value)} onKeyDown={onKeyDown} readOnly={busy} />
          <button type="submit" className={result ? '' : 'primary'} disabled={busy || !code.trim()}>
            {busy ? t('common.working') : t('scan.lookup')}
          </button>
          <button type="button" className={`camera-toggle ${camera ? 'active' : ''}`} onClick={toggleCamera}
            aria-pressed={camera} title={t(camera ? 'scan.camera.stop' : 'scan.camera.start')}>
            <Icon name="camera" size={18} />
            <span className="camera-toggle-label">{t(camera ? 'scan.camera.stop' : 'scan.camera.start')}</span>
          </button>
        </form>
        <BatchBar defaults={defaults} onChange={setDefaults} />
      </section>

      {error && <Alert tone="error">{error}</Alert>}

      {searching && (
        <section className="card scan-card scan-pending" aria-live="polite">
          <div className="scan-cover"><div className="cover" /></div>
          <div className="scan-info">
            <p className="scan-state"><code>{searching}</code></p>
            <h2 className="scan-title"><span className="spinner" aria-hidden="true" />{t('scan.searching')}</h2>
            <p className="muted small scan-explain">{t('scan.searchingHint')}</p>
          </div>
        </section>
      )}

      {result && (
        <ScanResult key={result.barcode} result={result} defaults={defaults} confirm={confirm}
          onOpenGame={setOpenId} onNext={next}
          onAdded={(a, game) => {
            putGame(game);
            setAdded((list) => [a, ...list].slice(0, 30));
            next();
          }} />
      )}

      {added.length > 0 && (
        <section className="scan-added">
          <h2 className="scan-added-title">{t('scan.added', { count: added.length })}</h2>
          <ul>
            {added.map((a) => (
              <li key={a.gameId + a.thumb}>
                <button onClick={() => setOpenId(a.gameId)} title={`${a.title} · ${a.platform}`}>
                  {a.thumb ? <img src={proxiedImage(a.thumb)} alt="" /> : <span className="thumb-placeholder" />}
                  <span className="scan-added-name">{a.title}</span>
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}

      {openId && <GameDetail key={openId} gameId={openId} onClose={() => setOpenId(null)} onOpenGame={setOpenId} />}
    </div>
  );
}

/** What is applied to every box of the batch, always visible; the fields open in place. */
function BatchBar({ defaults, onChange }: { defaults: Defaults; onChange: (d: Defaults) => void }) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);
  const value = (v: string, empty: string) => v || empty;
  return (
    <div className="batch">
      <button type="button" className="batch-summary" onClick={() => setEditing(!editing)} aria-expanded={editing}>
        <span className="batch-label">{t('scan.batch.label')}</span>
        <span className="batch-chip">{t('scan.batch.platform')}: <strong>{value(defaults.platform, t('scan.batch.auto'))}</strong></span>
        <span className="batch-chip">{t('copy.condition')}: <strong>{value(defaults.condition, '—')}</strong></span>
        <span className="batch-chip">{t('copy.location')}: <strong>{value(defaults.location, '—')}</strong></span>
        <span className="batch-edit-label">{editing ? t('scan.batch.done') : t('scan.batch.change')}</span>
      </button>
      {editing && (
        <div className="batch-fields">
          <label>
            {t('scan.batch.platform')}
            <input list="scan-platforms" value={defaults.platform} autoFocus
              onChange={(e) => onChange({ ...defaults, platform: e.target.value })} placeholder={t('scan.batch.platformAuto')} />
          </label>
          <label>
            {t('copy.condition')}
            <input value={defaults.condition} onChange={(e) => onChange({ ...defaults, condition: e.target.value })} />
          </label>
          <label>
            {t('copy.location')}
            <input value={defaults.location} onChange={(e) => onChange({ ...defaults, location: e.target.value })} placeholder={t('copy.locationPlaceholder')} />
          </label>
          <p className="muted small batch-hint">{t('scan.batch.hint')}</p>
        </div>
      )}
      <datalist id="scan-platforms">{PHYSICAL_PLATFORMS.map((p) => <option key={p} value={p} />)}</datalist>
    </div>
  );
}

function ScanResult({ result, defaults, confirm, onAdded, onNext, onOpenGame }: {
  result: IdentifyBarcodeResponse;
  defaults: Defaults;
  confirm: MutableRefObject<(() => void) | null>;
  onAdded: (a: Added, game: Game) => void;
  onNext: () => void;
  onOpenGame: (id: string) => void;
}) {
  const { t } = useTranslation();
  const { games } = useAppData();
  const m = result.match;
  // A product name without platform is usually not a game: the title field starts empty then.
  const [title, setTitle] = useState(result.suggestions[0]?.title ?? (m?.platform ? m.title : ''));
  const [platform, setPlatform] = useState(defaults.platform || m?.platform || '');
  const [edition, setEdition] = useState(m?.edition ?? '');
  const [suggestions, setSuggestions] = useState<GameSuggestion[]>(result.suggestions);
  const [existing, setExisting] = useState<GameRef[]>(result.existing);
  const [chosen, setChosen] = useState<GameSuggestion | null>(result.suggestions[0] ?? null);
  const [target, setTarget] = useState<string>(result.existing.length === 1 ? result.existing[0]!.id : '');
  // The title search starts open when there is nothing to add yet (an unknown code, or a product
  // without platform or without any matching game); otherwise only when asked.
  const [fixing, setFixing] = useState(!m || !(defaults.platform || m.platform) || result.suggestions.length === 0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [warnings, setWarnings] = useState<string[]>(result.warnings);

  const owned = result.owned[0];
  const canAdd = !owned && !busy && !!title.trim() && !!platform.trim();

  const search = async () => {
    if (!title.trim()) return;
    setBusy(true);
    setError('');
    try {
      const res = await lookupClient.suggestGames({ title, platform });
      setSuggestions(res.suggestions);
      setExisting(res.existing);
      setWarnings(res.warnings);
      setChosen(res.suggestions[0] ?? null);
      setTarget(res.existing.length === 1 ? res.existing[0]!.id : '');
      if (res.suggestions[0]) setTitle(res.suggestions[0].title);
      if (!platform && res.suggestions[0]?.platform) setPlatform(res.suggestions[0].platform);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const pick = (s: GameSuggestion) => {
    setChosen(s);
    setTitle(s.title);
    if (s.platform && !defaults.platform) setPlatform(s.platform);
  };

  const add = async () => {
    setBusy(true);
    setError('');
    const details = {
      ...emptyDetails(CopyKind.PHYSICAL), status: CopyStatus.OWNED, platform, edition,
      condition: defaults.condition, location: defaults.location, barcode: result.barcode,
    };
    try {
      const res = target
        ? await gameClient.addCopy({ gameId: target, details })
        : await gameClient.createGame({ title, coverUrl: chosen?.coverUrl ?? '', copies: [details] });
      onAdded({ gameId: res.game!.id, title: res.game!.title, platform, thumb: chosen?.thumbUrl ?? '' }, res.game!);
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  };

  // Enter in the barcode field confirms what this card offers.
  useEffect(() => {
    confirm.current = owned ? onNext : canAdd ? add : null;
    return () => { confirm.current = null; };
  });

  if (owned) {
    const game = games.find((g) => g.id === owned.game!.id);
    return (
      <section className="card scan-card">
        <div className="scan-cover">{game ? <Cover game={game} /> : <div className="cover" />}</div>
        <div className="scan-info">
          <p className="scan-state">{t('scan.owned')}</p>
          <h2 className="scan-title">{owned.game!.title}</h2>
          {owned.platform && <div className="scan-badges"><PlatformBadge platform={owned.platform} full /></div>}
        </div>
        <div className="scan-body">
          <div className="scan-actions">
            <button onClick={() => onOpenGame(owned.game!.id)}>{t('scan.openGame')}</button>
            <button className="primary" onClick={onNext}>{t('scan.next')}</button>
          </div>
          <p className="muted small scan-enter">{t('scan.enterHint')}</p>
        </div>
      </section>
    );
  }

  const provider = m ? PROVIDER_NAMES[m.providerId] ?? m.providerId : '';
  const existingTitle = existing.find((g) => g.id === target)?.title;
  // A copy added to a game you have keeps that game's cover; only a new game takes the chosen one.
  const targetGame = target ? games.find((g) => g.id === target) : undefined;
  const cover = chosen ? proxiedImage(chosen.thumbUrl || chosen.coverUrl) : '';

  return (
    <section className="card scan-card">
      <div className="scan-cover">
        {targetGame ? <Cover game={targetGame} /> : (
          <div className="cover">{cover ? <img src={cover} alt="" /> : <div className="cover-placeholder" aria-hidden="true"><Icon name="image" size={28} /></div>}</div>
        )}
      </div>
      <div className="scan-info">
        <p className="scan-state">
          <code>{result.barcode}</code>
          {m ? <> · {t('scan.foundIn', { provider, raw: m.raw })}</> : <> · {t('scan.unknownShort')}</>}
        </p>
        {/* Until a game is chosen, a database's product name (which may not be a game) stays in the line above. */}
        <h2 className="scan-title">{chosen ? title : t('scan.unknownTitle')}</h2>
        {(platform || edition) && (
          <div className="scan-badges">
            {platform && <PlatformBadge platform={platform} full />}
            {edition && <span className="scan-edition">{edition}</span>}
          </div>
        )}
        {!chosen && <p className="muted small scan-explain">{t('scan.unknown')}</p>}
      </div>

      <div className="scan-body">
        {fixing && (
          <form className="scan-fix" onSubmit={(e) => { e.preventDefault(); search(); }}>
            <label className="scan-fix-title">
              {t('game.title')}
              <input value={title} onChange={(e) => setTitle(e.target.value)} autoFocus required />
            </label>
            <label>
              {t('copy.platform')}
              <input list="scan-platforms" value={platform} onChange={(e) => setPlatform(e.target.value)} />
            </label>
            <label>
              {t('copy.edition')}
              <input value={edition} onChange={(e) => setEdition(e.target.value)} />
            </label>
            <button type="submit" disabled={busy || !title.trim()}>{t('scan.searchTitle')}</button>
          </form>
        )}

        {existing.length > 0 && (
          <div className="segmented scan-target" role="radiogroup" aria-label={t('scan.addTo')}>
            {existing.map((g) => (
              <button key={g.id} type="button" role="radio" aria-checked={target === g.id} className={target === g.id ? 'active' : ''}
                onClick={() => setTarget(g.id)}>{t('scan.existingGame', { title: g.title })}</button>
            ))}
            <button type="button" role="radio" aria-checked={target === ''} className={target === '' ? 'active' : ''}
              onClick={() => setTarget('')}>{t('scan.newGame')}</button>
          </div>
        )}

        {warnings.map((w) => <Alert key={w} tone="warn">{w}</Alert>)}
        {error && <Alert tone="error">{error}</Alert>}

        <div className="scan-actions">
          <button onClick={onNext} disabled={busy}>{t('scan.skip')}</button>
          <button className="primary" onClick={add} disabled={!canAdd}>
            {target ? t('scan.addCopyTo', { title: existingTitle }) : t('scan.addGame')}
          </button>
          {!fixing && <button className="link scan-fix-toggle" onClick={() => setFixing(true)}>{t('scan.fix')}</button>}
        </div>
        {canAdd && <p className="muted small scan-enter">{t('scan.enterHint')}</p>}
        {!canAdd && !busy && title.trim() && !platform.trim() && <p className="muted small scan-enter">{t('scan.needPlatform')}</p>}
      </div>

      {!target && suggestions.length > 1 && (
        <div className="scan-covers">
          <p className="muted small">{t('scan.otherCovers')}</p>
          <div className="scan-cover-strip">
            {suggestions.map((s) => (
              <button key={s.coverUrl} type="button" className={`scan-thumb ${chosen?.coverUrl === s.coverUrl ? 'selected' : ''}`}
                onClick={() => pick(s)} title={s.label || s.title} aria-pressed={chosen?.coverUrl === s.coverUrl}>
                <img src={proxiedImage(s.thumbUrl || s.coverUrl)} alt={s.label || s.title} loading="lazy" />
              </button>
            ))}
          </div>
        </div>
      )}
    </section>
  );
}
