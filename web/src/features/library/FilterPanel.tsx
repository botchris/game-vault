import { useEffect, useMemo, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { Icon } from '../../components/Icon';
import { CopyKind, KINDS, PLAY_STATUSES, PlayStatus, kindKey, playKey, type Game } from '../../lib/model';
import { useAppData } from '../../state/AppData';

export interface Filters {
  kind: CopyKind;
  platforms: string[];
  genres: string[];
  /** Source ids; MANUAL stands for copies added by hand or from a CSV. */
  sources: string[];
  /** Play statuses; UNSPECIFIED stands for games the user has not given one. */
  play: PlayStatus[];
}

export const MANUAL = '';

export const NO_FILTERS: Filters = { kind: CopyKind.UNSPECIFIED, platforms: [], genres: [], sources: [], play: [] };

export const activeFilterCount = (f: Filters) => (f.kind ? 1 : 0) + f.platforms.length + f.genres.length + f.sources.length + f.play.length;

/** A game matches when it has a copy of the kind on one of the platforms, a copy from one of the
 * sources, one of the genres and one of the play statuses. */
export function matchesFilters(g: Game, f: Filters): boolean {
  if (f.kind || f.platforms.length) {
    const ok = g.copies.some((c) => (!f.kind || c.details?.kind === f.kind) && (!f.platforms.length || f.platforms.includes(c.details?.platform ?? '')));
    if (!ok) return false;
  }
  if (f.sources.length && !g.copies.some((c) => f.sources.includes(c.sourceId))) return false;
  if (f.genres.length && !g.genres.some((x) => f.genres.includes(x))) return false;
  if (f.play.length && !f.play.includes(g.playStatus)) return false;
  return true;
}

function toggle<T>(list: T[], v: T) {
  return list.includes(v) ? list.filter((x) => x !== v) : [...list, v];
}

/**
 * Filters popover (desktop) / bottom sheet (phones). Options show how many games each would match,
 * so empty choices are obvious before picking them.
 */
export default function FilterPanel({ games, filters, onChange, onClose, detailsCached }: {
  games: Game[];
  filters: Filters;
  onChange: (f: Filters) => void;
  onClose: () => void;
  detailsCached: number;
}) {
  const { t, i18n } = useTranslation();
  const panel = useRef<HTMLDivElement>(null);
  const { sources: configured } = useAppData();

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    const onDown = (e: PointerEvent) => {
      const target = e.target as Node;
      if (panel.current && !panel.current.contains(target) && !(target as HTMLElement).closest?.('.filter-button')) onClose();
    };
    window.addEventListener('keydown', onKey);
    window.addEventListener('pointerdown', onDown);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('pointerdown', onDown);
    };
  }, [onClose]);

  const { platforms, genres, sources, play } = useMemo(() => {
    const pl = new Map<PlayStatus, number>();
    const p = new Map<string, number>();
    const g = new Map<string, number>();
    const s = new Map<string, number>();
    for (const game of games) {
      pl.set(game.playStatus, (pl.get(game.playStatus) ?? 0) + 1);
      for (const id of new Set(game.copies.map((c) => c.sourceId))) s.set(id, (s.get(id) ?? 0) + 1);
      for (const pl of new Set(game.copies.map((c) => c.details?.platform).filter(Boolean) as string[])) p.set(pl, (p.get(pl) ?? 0) + 1);
      for (const ge of game.genres) g.set(ge, (g.get(ge) ?? 0) + 1);
    }
    const sorted = (m: Map<string, number>) => [...m.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0], i18n.language));
    // Configured sources first (in their own order), then manual copies; sources with no copies are hidden.
    const src = configured.filter((x) => s.has(x.id)).map((x) => ({ id: x.id, name: x.name, count: s.get(x.id)! }));
    if (s.has(MANUAL)) src.push({ id: MANUAL, name: '', count: s.get(MANUAL)! });
    // The statuses in their own order, then games without one; the ones no game has are hidden.
    const played = [...PLAY_STATUSES, PlayStatus.UNSPECIFIED].filter((x) => pl.has(x)).map((x) => ({ status: x, count: pl.get(x)! }));
    return { platforms: sorted(p), genres: sorted(g), sources: src, play: played };
  }, [games, configured, i18n.language]);

  return (
    <>
      <div className="sheet-backdrop" onClick={onClose} />
      <div className="filter-panel" ref={panel} role="dialog" aria-label={t('library.filters.button')}>
        <header className="filter-head">
          <h2>{t('library.filters.button')}</h2>
          {activeFilterCount(filters) > 0 && <button className="link" onClick={() => onChange(NO_FILTERS)}>{t('library.filters.clear')}</button>}
          <button className="icon-button" onClick={onClose} aria-label={t('common.close')}><Icon name="close" size={18} /></button>
        </header>

        <section>
          <h3>{t('library.filters.kind')}</h3>
          <div className="choice-row">
            <button className={`choice ${!filters.kind ? 'active' : ''}`} onClick={() => onChange({ ...filters, kind: CopyKind.UNSPECIFIED })}>{t('library.filters.any')}</button>
            {KINDS.map((k) => (
              <button key={k} className={`choice kind-${kindKey(k)} ${filters.kind === k ? 'active' : ''}`} aria-pressed={filters.kind === k}
                onClick={() => onChange({ ...filters, kind: filters.kind === k ? CopyKind.UNSPECIFIED : k })}>{t(`kind.${kindKey(k)}`)}</button>
            ))}
          </div>
        </section>

        {play.length > 1 && (
          <section>
            <h3>{t('play.label')}</h3>
            <div className="choice-row">
              {play.map((x) => (
                <button key={x.status} className={`choice ${filters.play.includes(x.status) ? 'active' : ''}`} aria-pressed={filters.play.includes(x.status)}
                  onClick={() => onChange({ ...filters, play: toggle(filters.play, x.status) })}>
                  {t(`play.${playKey(x.status)}`)}<span className="choice-count">{x.count}</span>
                </button>
              ))}
            </div>
          </section>
        )}

        {sources.length > 1 && (
          <section>
            <h3>{t('library.filters.sources')}</h3>
            <div className="choice-row">
              {sources.map((x) => (
                <button key={x.id} className={`choice ${filters.sources.includes(x.id) ? 'active' : ''}`} aria-pressed={filters.sources.includes(x.id)}
                  onClick={() => onChange({ ...filters, sources: toggle(filters.sources, x.id) })}>
                  {x.id === MANUAL ? t('library.filters.manual') : x.name}<span className="choice-count">{x.count}</span>
                </button>
              ))}
            </div>
          </section>
        )}

        <section>
          <h3>{t('copy.platform')}</h3>
          <div className="choice-row">
            {platforms.map(([p, n]) => (
              <button key={p} className={`choice ${filters.platforms.includes(p) ? 'active' : ''}`} aria-pressed={filters.platforms.includes(p)}
                onClick={() => onChange({ ...filters, platforms: toggle(filters.platforms, p) })}>{p}<span className="choice-count">{n}</span></button>
            ))}
          </div>
        </section>

        <section>
          <h3>{t('library.filters.genres')}</h3>
          {genres.length === 0 ? <p className="muted small">{t('library.filters.noGenres')}</p> : (
            <div className="choice-row">
              {genres.map(([g, n]) => (
                <button key={g} className={`choice ${filters.genres.includes(g) ? 'active' : ''}`} aria-pressed={filters.genres.includes(g)}
                  onClick={() => onChange({ ...filters, genres: toggle(filters.genres, g) })}>{g}<span className="choice-count">{n}</span></button>
              ))}
            </div>
          )}
          {detailsCached < games.length && (
            <p className="muted small">{t('library.filters.genresPartial', { cached: detailsCached.toLocaleString(i18n.language), total: games.length.toLocaleString(i18n.language) })}</p>
          )}
        </section>

        <footer className="filter-foot">
          <button className="primary" onClick={onClose}>{t('library.filters.done')}</button>
        </footer>
      </div>
    </>
  );
}
