import { lazy, Suspense, useEffect, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, gameClient, lookupClient, proxiedImage } from '../../api/client';
import { Alert } from '../../components/ui';
import type { GameRef, GameSuggestion, IdentifyBarcodeResponse } from '../../gen/gamevault/v1/lookup_pb';
import { CopyKind, CopyStatus, PHYSICAL_PLATFORMS, emptyDetails, validBarcode, type Game } from '../../lib/model';
import { useAppData } from '../../state/AppData';
import GameDetail from '../library/GameDetail';

const CameraScanner = lazy(() => import('./CameraScanner'));

const DEFAULTS_KEY = 'gamevault.scanDefaults';

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

interface Added {
  gameId: string;
  title: string;
  platform: string;
  thumb: string;
}

/**
 * Quick registration of physical games: scan (camera, USB/Bluetooth reader or typing) → confirm →
 * next. Unknown barcodes are identified by title once and remembered through the copy's barcode.
 */
export default function ScanPage() {
  const { t } = useTranslation();
  const [openId, setOpenId] = useState<string | null>(null);
  const onOpenGame = setOpenId;
  const { putGame } = useAppData();
  const [code, setCode] = useState('');
  const [camera, setCamera] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState<IdentifyBarcodeResponse | null>(null);
  const [defaults, setDefaults] = useState<Defaults>(loadDefaults);
  const [added, setAdded] = useState<Added[]>([]);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    try {
      localStorage.setItem(DEFAULTS_KEY, JSON.stringify(defaults));
    } catch {
      /* not remembered in private mode */
    }
  }, [defaults]);

  const identify = async (raw: string) => {
    if (!validBarcode(raw)) {
      setError(t('scan.invalid', { code: raw }));
      return;
    }
    setBusy(true);
    setError('');
    setResult(null);
    try {
      setResult(await lookupClient.identifyBarcode({ barcode: raw }));
      setCode('');
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const reset = () => {
    setResult(null);
    setError('');
    input.current?.focus();
  };

  const submitCode = (e: FormEvent) => {
    e.preventDefault();
    if (code.trim()) identify(code);
  };

  return (
    <div className="page scan">
      <div>
        <h2>{t('scan.title')}</h2>
        <p className="muted">{t('scan.intro')}</p>
      </div>

      <section className="card">
        <form className="row" onSubmit={submitCode}>
          <input ref={input} className="barcode-input" inputMode="numeric" autoFocus autoComplete="off" placeholder={t('scan.placeholder')}
            value={code} onChange={(e) => setCode(e.target.value)} disabled={busy} />
          <button type="submit" className="primary" disabled={busy || !code.trim()}>{busy ? t('common.working') : t('scan.lookup')}</button>
          <button type="button" onClick={() => setCamera(!camera)}>{camera ? t('scan.camera.stop') : `📷 ${t('scan.camera.start')}`}</button>
        </form>
        {camera && (
          <Suspense fallback={<p className="muted">{t('common.loading')}</p>}>
            <CameraScanner paused={busy || result !== null} onCode={identify} />
          </Suspense>
        )}
        <details className="defaults">
          <summary>{t('scan.defaults.title')}</summary>
          <p className="muted small">{t('scan.defaults.hint')}</p>
          <div className="grid grid-3">
            <label>
              {t('scan.defaults.platform')}
              <input list="scan-platforms" value={defaults.platform} onChange={(e) => setDefaults({ ...defaults, platform: e.target.value })} placeholder={t('scan.defaults.platformAuto')} />
            </label>
            <label>
              {t('copy.condition')}
              <input value={defaults.condition} onChange={(e) => setDefaults({ ...defaults, condition: e.target.value })} />
            </label>
            <label>
              {t('copy.location')}
              <input value={defaults.location} onChange={(e) => setDefaults({ ...defaults, location: e.target.value })} placeholder={t('copy.locationPlaceholder')} />
            </label>
          </div>
        </details>
        <datalist id="scan-platforms">{PHYSICAL_PLATFORMS.map((p) => <option key={p} value={p} />)}</datalist>
      </section>

      {error && <Alert tone="error">{error}</Alert>}

      {result && (
        <ScanResult key={result.barcode} result={result} defaults={defaults} onOpenGame={onOpenGame} onCancel={reset}
          onAdded={(a, game) => {
            putGame(game);
            setAdded((list) => [a, ...list].slice(0, 30));
            reset();
          }} />
      )}

      {openId && <GameDetail key={openId} gameId={openId} onClose={() => setOpenId(null)} onOpenGame={setOpenId} />}

      {added.length > 0 && (
        <section className="card">
          <h3>{t('scan.added', { count: added.length })}</h3>
          <ul className="added-list">
            {added.map((a) => (
              <li key={a.gameId + a.thumb}>
                <button onClick={() => onOpenGame(a.gameId)}>
                  {a.thumb ? <img src={proxiedImage(a.thumb)} alt="" /> : <span className="thumb-placeholder" />}
                  <span>{a.title} <span className="muted small">· {a.platform}</span></span>
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

function ScanResult({ result, defaults, onAdded, onCancel, onOpenGame }: {
  result: IdentifyBarcodeResponse;
  defaults: Defaults;
  onAdded: (a: Added, game: Game) => void;
  onCancel: () => void;
  onOpenGame: (id: string) => void;
}) {
  const { t } = useTranslation();
  const m = result.match;
  const [title, setTitle] = useState(result.suggestions[0]?.title ?? m?.title ?? '');
  const [platform, setPlatform] = useState(defaults.platform || m?.platform || '');
  const [edition, setEdition] = useState(m?.edition ?? '');
  const [suggestions, setSuggestions] = useState<GameSuggestion[]>(result.suggestions);
  const [existing, setExisting] = useState<GameRef[]>(result.existing);
  const [chosen, setChosen] = useState<GameSuggestion | null>(result.suggestions[0] ?? null);
  const [target, setTarget] = useState<string>(result.existing.length === 1 ? result.existing[0]!.id : '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [warnings, setWarnings] = useState<string[]>(result.warnings);

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
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const pick = (s: GameSuggestion) => {
    setChosen(s);
    setTitle(s.title);
    if (s.platform) setPlatform(s.platform);
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

  if (result.owned.length > 0) {
    return (
      <section className="card scan-result">
        <Alert tone="warn">
          {t('scan.owned', { title: result.owned[0]!.game!.title, platform: result.owned[0]!.platform })}
        </Alert>
        <div className="actions">
          <button onClick={() => onOpenGame(result.owned[0]!.game!.id)}>{t('scan.openGame')}</button>
          <span className="spacer" />
          <button className="primary" onClick={onCancel} autoFocus>{t('scan.next')}</button>
        </div>
      </section>
    );
  }

  return (
    <section className="card scan-result">
      <p className="small muted"><code>{result.barcode}</code>{m && <> · {t('scan.foundAs', { raw: m.raw })}</>}</p>
      {!m && <Alert tone="warn">{t('scan.unknown')}</Alert>}
      {warnings.map((w) => <Alert key={w} tone="warn">{w}</Alert>)}

      <form className="grid grid-3" onSubmit={(e) => { e.preventDefault(); search(); }}>
        <label className="span2">
          {t('game.title')}
          <input value={title} onChange={(e) => setTitle(e.target.value)} autoFocus={!m} required />
        </label>
        <label>
          {t('copy.platform')}
          <input list="scan-platforms" value={platform} onChange={(e) => setPlatform(e.target.value)} required />
        </label>
        <label>
          {t('copy.edition')}
          <input value={edition} onChange={(e) => setEdition(e.target.value)} />
        </label>
        <div className="actions span2">
          <button type="submit" disabled={busy || !title.trim()}>{t('scan.searchTitle')}</button>
        </div>
      </form>

      {suggestions.length > 0 && (
        <div className="posters picker-grid">
          {suggestions.map((s) => (
            <button key={s.coverUrl} type="button" className={`poster ${chosen?.coverUrl === s.coverUrl ? 'selected' : ''}`} onClick={() => pick(s)} title={s.label}>
              <div className="cover"><img src={proxiedImage(s.thumbUrl || s.coverUrl)} alt="" loading="lazy" /></div>
              <div className="poster-caption"><span className="poster-title">{s.label || s.title}</span></div>
            </button>
          ))}
        </div>
      )}

      {existing.length > 0 && (
        <fieldset className="targets">
          <legend>{t('scan.addTo')}</legend>
          <label className="check-inline"><input type="radio" checked={target === ''} onChange={() => setTarget('')} /> {t('scan.newGame')}</label>
          {existing.map((g) => (
            <label key={g.id} className="check-inline">
              <input type="radio" checked={target === g.id} onChange={() => setTarget(g.id)} /> {t('scan.existingGame', { title: g.title })}
            </label>
          ))}
        </fieldset>
      )}

      {error && <Alert tone="error">{error}</Alert>}
      <div className="actions">
        <button onClick={onCancel} disabled={busy}>{t('scan.skip')}</button>
        <span className="spacer" />
        <button className="primary" onClick={add} disabled={busy || !title.trim() || !platform.trim()}>
          {target ? t('scan.addCopy') : t('scan.addGame')}
        </button>
      </div>
    </section>
  );
}
