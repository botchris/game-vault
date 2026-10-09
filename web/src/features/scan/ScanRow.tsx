import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, lookupClient, proxiedImage } from '../../api/client';
import { Cover } from '../../components/Cover';
import { Icon } from '../../components/Icon';
import { PlatformBadge } from '../../components/PlatformBadge';
import { Alert } from '../../components/ui';
import { useAppData } from '../../state/AppData';
import { resolved, status, type Answer, type Choice, type Row } from './scanList.ts';

/** Names of the barcode databases, for "found in …". */
const PROVIDER_NAMES: Record<string, string> = { cex: 'CeX', ebay: 'eBay', upcitemdb: 'UPCitemdb', eansearch: 'EAN-Search' };

export function ScanRow({ row, platform, open, onToggle, onPlus, onRemove, onRetry, onChoose }: {
  row: Row;
  /** The batch platform ('' when the code's is used). */
  platform: string;
  open: boolean;
  onToggle: () => void;
  onPlus: () => void;
  onRemove: () => void;
  onRetry: () => void;
  onChoose: (c: Choice) => void;
}) {
  const { t } = useTranslation();
  const { games } = useAppData();
  const st = status(row, platform);
  const c = resolved(row, platform);
  const target = c?.gameId ? games.find((g) => g.id === c.gameId) : undefined;
  const title = c?.title || row.answer?.match?.raw || row.code;
  const thumb = c?.thumbUrl || c?.coverUrl;
  const state = st === 'ready'
    ? (c?.gameId ? t('scan.list.copyOf', { title: target?.title ?? c.title }) : t('scan.list.newGame'))
    : t(`scan.list.${st}`);
  const canOpen = row.phase === 'done' && st !== 'owned';

  return (
    <li className={`scan-row is-${st} ${open ? 'open' : ''}`}>
      <div className="scan-row-line">
        <button type="button" className="scan-row-main" onClick={onToggle} disabled={!canOpen} aria-expanded={canOpen ? open : undefined}>
          <span className="scan-row-thumb">
            {target ? <Cover game={target} /> : thumb ? <img src={proxiedImage(thumb)} alt="" /> : <span className="thumb-placeholder" />}
          </span>
          <span className="scan-row-text">
            <span className="scan-row-title">{st === 'looking' ? <code>{row.code}</code> : title}</span>
            <span className="scan-row-meta">
              {c?.platform && <PlatformBadge platform={c.platform} />}
              <span className={`scan-row-state state-${st}`}>{st === 'looking' && <span className="spinner" aria-hidden="true" />}{state}</span>
              {row.count > 1 && <span className="scan-row-count">{t('scan.list.count', { count: row.count })}</span>}
            </span>
            {row.error && <span className="scan-row-error">{row.error}</span>}
          </span>
        </button>
        <div className="scan-row-actions">
          {st === 'error' && row.phase === 'error' && <button type="button" className="small-button" onClick={onRetry}>{t('scan.list.retry')}</button>}
          <button type="button" className="small-button" onClick={onPlus} title={t('scan.list.plusOneTitle')}>{t('scan.list.plusOne')}</button>
          <button type="button" className="icon-button" onClick={onRemove} aria-label={t('scan.list.remove')} title={t('scan.list.remove')}>
            <Icon name="close" size={16} />
          </button>
        </div>
      </div>
      {open && canOpen && row.answer && c && <RowDetail key={row.id} answer={row.answer} choice={c} batchPlatform={platform} onChoose={onChoose} onDone={onToggle} />}
    </li>
  );
}

/** The expanded row: what today's result card offered, editing the row's choice as you go. */
function RowDetail({ answer, choice, batchPlatform, onChoose, onDone }: {
  answer: Answer;
  choice: Choice;
  batchPlatform: string;
  onChoose: (c: Choice) => void;
  onDone: () => void;
}) {
  const { t } = useTranslation();
  const [suggestions, setSuggestions] = useState(answer.suggestions);
  const [existing, setExisting] = useState(answer.existing);
  const [warnings, setWarnings] = useState(answer.warnings);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const m = answer.match;
  const set = (patch: Partial<Choice>) => onChoose({ ...choice, ...patch });

  const search = async () => {
    if (!choice.title.trim()) return;
    setBusy(true);
    setError('');
    try {
      const res = await lookupClient.suggestGames({ title: choice.title, platform: choice.platform });
      const s = res.suggestions.map((x) => ({ title: x.title, platform: x.platform, coverUrl: x.coverUrl, thumbUrl: x.thumbUrl, label: x.label }));
      setSuggestions(s);
      setExisting(res.existing.map((g) => ({ id: g.id, title: g.title })));
      setWarnings(res.warnings);
      const first = s[0];
      set({
        title: first?.title ?? choice.title,
        platform: choice.platform || first?.platform || '',
        coverUrl: first?.coverUrl ?? '',
        thumbUrl: first?.thumbUrl ?? '',
        gameId: res.existing.length === 1 ? res.existing[0]!.id : '',
      });
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="scan-row-detail">
      <p className="muted small scan-row-found">
        <code>{answer.barcode}</code>{' · '}
        {m ? t('scan.list.foundIn', { provider: PROVIDER_NAMES[m.providerId] ?? m.providerId, raw: m.raw }) : t('scan.list.unknown')}
      </p>
      <form className="scan-fix" onSubmit={(e) => { e.preventDefault(); search(); }}>
        <label className="scan-fix-title">
          {t('game.title')}
          <input value={choice.title} onChange={(e) => set({ title: e.target.value })} required />
        </label>
        <label>
          {t('copy.platform')}
          <input list="scan-platforms" value={choice.platform} disabled={!!batchPlatform} onChange={(e) => set({ platform: e.target.value })} />
        </label>
        <label>
          {t('copy.edition')}
          <input value={choice.edition} onChange={(e) => set({ edition: e.target.value })} />
        </label>
        <button type="submit" disabled={busy || !choice.title.trim()}>{busy ? t('common.working') : t('scan.list.search')}</button>
      </form>

      {existing.length > 0 && (
        <div className="segmented scan-target" role="radiogroup" aria-label={t('scan.list.addTo')}>
          {existing.map((g) => (
            <button key={g.id} type="button" role="radio" aria-checked={choice.gameId === g.id} className={choice.gameId === g.id ? 'active' : ''}
              onClick={() => set({ gameId: g.id })}>{t('scan.list.copyOf', { title: g.title })}</button>
          ))}
          <button type="button" role="radio" aria-checked={choice.gameId === ''} className={choice.gameId === '' ? 'active' : ''}
            onClick={() => set({ gameId: '' })}>{t('scan.list.newGame')}</button>
        </div>
      )}

      {!choice.gameId && suggestions.length > 0 && (
        <div className="scan-covers">
          <p className="muted small">{t('scan.list.cover')}</p>
          <div className="scan-cover-strip">
            {suggestions.map((s) => (
              <button key={s.coverUrl} type="button" className={`scan-thumb ${choice.coverUrl === s.coverUrl ? 'selected' : ''}`}
                onClick={() => set({ title: s.title, coverUrl: s.coverUrl, thumbUrl: s.thumbUrl, platform: choice.platform || s.platform })}
                title={s.label || s.title} aria-pressed={choice.coverUrl === s.coverUrl}>
                <img src={proxiedImage(s.thumbUrl || s.coverUrl)} alt={s.label || s.title} loading="lazy" />
              </button>
            ))}
          </div>
        </div>
      )}

      {warnings.map((w) => <Alert key={w} tone="warn">{w}</Alert>)}
      {error && <Alert tone="error">{error}</Alert>}
      <div className="scan-actions"><button type="button" className="primary" onClick={onDone}>{t('scan.list.done')}</button></div>
    </div>
  );
}
