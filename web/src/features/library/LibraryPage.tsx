import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, gameClient } from '../../api/client';
import { Cover } from '../../components/Cover';
import { Icon } from '../../components/Icon';
import { Alert, useFormatters } from '../../components/ui';
import { searchableText } from '../../lib/fields';
import { CopyStatus, PlayStatus, daysUntil, isPendingKey, nextDeadline, playKey, toDate, type Game } from '../../lib/model';
import { useAppData } from '../../state/AppData';
import {
  NO_FILTERS, activeFilterCount, filterItems, itemCounts, libraryItems, pruneFilters, sortItems, type Filters, type Grouping, type LibraryItem,
} from '../../lib/editions';
import FilterPanel from './FilterPanel';
import GameDetail, { type SheetNav } from './GameDetail';
import NewGameDialog from './NewGameDialog';
import { Stars } from './PlayControls';
import PosterGrid, { ItemBadges } from './PosterGrid';

type Quick = 'pending' | 'expiring' | 'redundant' | null;
export type SortBy = 'titleAsc' | 'titleDesc' | 'added' | 'year' | 'deadline' | 'copies' | 'rating';
type View = 'list' | 'posters';

const SORTS: SortBy[] = ['titleAsc', 'titleDesc', 'added', 'year', 'deadline', 'copies', 'rating'];
const CHUNK = 120;
const EXPIRING_DAYS = 30;
const LETTERS = ['#', ...'ABCDEFGHIJKLMNOPQRSTUVWXYZ'];

/** Remembered per device: purely a viewing preference. */
function usePref<T extends string>(key: string, fallback: T, allowed: readonly T[]): [T, (v: T) => void] {
  const [value, setValue] = useState<T>(() => {
    try {
      const v = localStorage.getItem(key) as T | null;
      return v && allowed.includes(v) ? v : fallback;
    } catch {
      return fallback;
    }
  });
  const set = (v: T) => {
    setValue(v);
    try {
      localStorage.setItem(key, v);
    } catch {
      /* private mode: not remembered */
    }
  };
  return [value, set];
}

/** First letter for the A–Z bar: accents folded, digits and symbols under "#". */
export function initialOf(title: string): string {
  const c = title.normalize('NFD').replace(/[̀-ͯ]/g, '').match(/[A-Za-z0-9]/)?.[0]?.toUpperCase() ?? '#';
  return /[A-Z]/.test(c) ? c : '#';
}

const isExpiring = (g: Game) => {
  const d = daysUntil(nextDeadline(g));
  return d !== null && d <= EXPIRING_DAYS;
};

export default function LibraryPage() {
  const { t, i18n } = useTranslation();
  const { games, reloadGames, detailsCached, fields } = useAppData();
  const [q, setQ] = useState('');
  const [quick, setQuick] = useState<Quick>(null);
  const [letter, setLetter] = useState('');
  const [chosenFilters, setFilters] = useState<Filters>(NO_FILTERS);
  // Values of fields deleted or choices removed since they were picked no longer filter or count.
  const filters = useMemo(() => pruneFilters(chosenFilters, fields), [chosenFilters, fields]);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [sortBy, setSortBy] = usePref<SortBy>('gamevault.librarySort', 'titleAsc', SORTS);
  const [view, setView] = usePref<View>('gamevault.libraryView', 'posters', ['list', 'posters']);
  const [grouping, setGrouping] = usePref<Grouping>('gamevault.libraryGrouping', 'platform', ['platform', 'game']);
  const [shown, setShown] = useState(CHUNK);
  // The open sheet: a game and the edition it was opened on ('' for the main one).
  const [open, setOpen] = useState<{ id: string; system: string } | null>(null);
  const [creating, setCreating] = useState(false);
  const [notice, setNotice] = useState<{ tone: 'ok' | 'error'; text: string } | null>(null);
  const sentinel = useRef<HTMLDivElement>(null);

  // The library's items: one per edition ("By platform") or per game ("By game"); filters, counts and
  // sorts look at each item's view (in "By platform", the game with only that edition's copies).
  const items = useMemo(() => libraryItems(games, grouping), [games, grouping]);
  const views = useMemo(() => items.map((i) => i.view), [items]);

  const counts = useMemo(() => {
    const copies = games.flatMap((g) => g.copies);
    return {
      pending: views.filter((g) => g.copies.some(isPendingKey)).length,
      expiring: views.filter(isExpiring).length,
      redundant: views.filter((g) => g.copies.some((c) => c.redundant)).length,
      revealedRedundant: copies.filter((c) => c.redundant && c.details?.status === CopyStatus.REVEALED).length,
    };
  }, [games, views]);

  // Everything but the letter, so the A–Z bar can show which letters have games.
  const base = useMemo(() => {
    const needle = q.trim().toLocaleLowerCase(i18n.language);
    const quickOk = (v: Game) => (quick === 'pending' ? v.copies.some(isPendingKey)
      : quick === 'expiring' ? isExpiring(v)
        : quick === 'redundant' ? v.copies.some((c) => c.redundant) : true);
    const searchOk = (g: Game) => !needle || [g.title, g.notes, ...g.genres,
      ...g.copies.flatMap((c) => [c.details?.origin, c.details?.notes, c.details?.location, c.details?.edition]), searchableText(g, fields)]
      .join(' ').toLocaleLowerCase(i18n.language).includes(needle);
    return filterItems(items, filters, fields, { copyLevel: quickOk, gameLevel: searchOk });
  }, [items, q, quick, filters, fields, i18n.language]);

  const lettersWithGames = useMemo(() => new Set(base.map((i) => initialOf(i.game.title))), [base]);

  const filtered = useMemo(() => {
    const byTitle = (a: Game, b: Game) => a.title.localeCompare(b.title, i18n.language, { sensitivity: 'base', numeric: true });
    const cmp: Record<SortBy, (a: Game, b: Game) => number> = {
      titleAsc: byTitle,
      titleDesc: (a, b) => byTitle(b, a),
      added: (a, b) => (toDate(b.createdAt)?.getTime() ?? 0) - (toDate(a.createdAt)?.getTime() ?? 0),
      year: (a, b) => (b.releaseYear || 0) - (a.releaseYear || 0) || byTitle(a, b),
      deadline: (a, b) => (nextDeadline(a) || '9999').localeCompare(nextDeadline(b) || '9999') || byTitle(a, b),
      copies: (a, b) => b.copies.length - a.copies.length || byTitle(a, b),
      rating: (a, b) => b.rating - a.rating || byTitle(a, b),
    };
    return sortItems(base.filter((i) => !letter || initialOf(i.game.title) === letter), cmp[sortBy]);
  }, [base, letter, sortBy, i18n.language]);

  // Reset the window when the result changes; grow it as the user scrolls.
  useEffect(() => setShown(CHUNK), [q, quick, letter, filters, sortBy, grouping]);
  useEffect(() => {
    const el = sentinel.current;
    if (!el) return;
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) setShown((n) => n + CHUNK);
    }, { rootMargin: '800px' });
    io.observe(el);
    return () => io.disconnect();
  }, [filtered.length, view]);

  const visible = filtered.slice(0, shown);

  // The open sheet moves through the list as the library shows it (search, filters, sort), loading
  // the grid down to that item so it is there when the sheet closes.
  const openIndex = open ? filtered.findIndex((i) => i.game.id === open.id && (grouping === 'game' || i.system === open.system)) : -1;
  const openAt = (i: number) => {
    setOpen({ id: filtered[i]!.game.id, system: filtered[i]!.system });
    if (i >= shown) setShown(Math.ceil((i + 1) / CHUNK) * CHUNK);
  };
  const nav: SheetNav | undefined = openIndex < 0 ? undefined : {
    previous: openIndex > 0 ? () => openAt(openIndex - 1) : undefined,
    next: openIndex < filtered.length - 1 ? () => openAt(openIndex + 1) : undefined,
  };

  // Clicking a system badge shows only that system; clicking it again clears the filter.
  const filterSystem = (system: string) => setFilters((f) => ({
    ...f, systems: f.systems.length === 1 && f.systems[0] === system ? [] : [system],
  }));
  const nFilters = activeFilterCount(filters);

  const markRedeemed = async () => {
    try {
      const res = await gameClient.markRedeemedKeys({});
      await reloadGames();
      setNotice({ tone: 'ok', text: t('library.markedRedeemed', { count: res.updated }) });
    } catch (e) {
      setNotice({ tone: 'error', text: errorMessage(e) });
    }
  };

  const quickChips: { key: Exclude<Quick, null>; count: number; tone?: string }[] = [
    { key: 'pending', count: counts.pending },
    { key: 'expiring', count: counts.expiring, tone: counts.expiring ? 'danger' : undefined },
    { key: 'redundant', count: counts.redundant, tone: counts.redundant ? 'warn' : undefined },
  ];

  return (
    <div className="library">
      <header className="page-head">
        <h1 className="page-title">{t('nav.library')}</h1>
        <span className="page-count">{grouping === 'platform'
          ? t('library.showingEditions', { editions: filtered.length.toLocaleString(i18n.language), games: itemCounts(filtered).games.toLocaleString(i18n.language) })
          : t('library.showing', { shown: filtered.length.toLocaleString(i18n.language), total: games.length.toLocaleString(i18n.language) })}</span>
        <span className="spacer" />
        <button className="primary" onClick={() => setCreating(true)}><Icon name="plus" size={18} /><span className="hide-narrow">{t('library.addGame')}</span></button>
      </header>

      <div className="toolbar library-toolbar">
        <label className="search-field">
          <Icon name="search" size={18} />
          <input type="search" placeholder={t('library.search')} value={q} onChange={(e) => setQ(e.target.value)} aria-label={t('library.search')} />
        </label>
        <select className="sort-select" value={sortBy} onChange={(e) => setSortBy(e.target.value as SortBy)} aria-label={t('library.sort.label')}>
          {SORTS.map((s) => <option key={s} value={s}>{t(`library.sort.${s}`)}</option>)}
        </select>
        <div className="filter-anchor">
          <button className={`filter-button ${nFilters ? 'has-filters' : ''}`} onClick={() => setFiltersOpen((o) => !o)} aria-expanded={filtersOpen}>
            <Icon name="filter" size={18} />
            <span className="hide-narrow">{t('library.filters.button')}</span>
            {nFilters > 0 && <span className="filter-count">{nFilters}</span>}
          </button>
          {filtersOpen && (
            <FilterPanel games={views} total={games.length} filters={filters} onChange={setFilters} onClose={() => setFiltersOpen(false)}
              detailsCached={detailsCached} />
          )}
        </div>
        <div className="segmented grouping-toggle" role="group" aria-label={t('library.grouping.label')}>
          {(['platform', 'game'] as const).map((g) => (
            <button key={g} className={grouping === g ? 'active' : ''} aria-pressed={grouping === g} onClick={() => setGrouping(g)}>
              {t(`library.grouping.${g}`)}
            </button>
          ))}
        </div>
        <div className="segmented view-toggle" role="group" aria-label={t('library.view.label')}>
          <button className={view === 'posters' ? 'active' : ''} onClick={() => setView('posters')} title={t('library.view.posters')} aria-label={t('library.view.posters')}><Icon name="grid" size={18} /></button>
          <button className={view === 'list' ? 'active' : ''} onClick={() => setView('list')} title={t('library.view.list')} aria-label={t('library.view.list')}><Icon name="list" size={18} /></button>
        </div>
      </div>

      <div className="quick-row" role="group" aria-label={t('library.quick.label')}>
        {quickChips.map((c) => (
          <button key={c.key} className={`quick-chip ${quick === c.key ? 'active' : ''} ${c.tone ?? ''}`} aria-pressed={quick === c.key}
            onClick={() => setQuick(quick === c.key ? null : c.key)} disabled={c.count === 0 && quick !== c.key}>
            {t(`library.quick.${c.key}`)}<span className="quick-count">{c.count}</span>
          </button>
        ))}
        {(nFilters > 0 || letter || quick || q) && (
          <button className="link clear-all" onClick={() => { setFilters(NO_FILTERS); setLetter(''); setQuick(null); setQ(''); }}>{t('library.filters.clearAll')}</button>
        )}
      </div>

      {quick === 'redundant' && counts.revealedRedundant > 0 && (
        <div className="hint-bar">
          <span>{t('library.markRedeemedHelp')}</span>
          <button onClick={markRedeemed}>{t('library.markRedeemed', { count: counts.revealedRedundant })}</button>
        </div>
      )}
      {notice && <Alert tone={notice.tone}>{notice.text}</Alert>}

      <div className={`library-body view-${view}`}>
        <nav className="alpha-rail" aria-label={t('library.alpha')}>
          {LETTERS.map((l) => (
            <button key={l} className={letter === l ? 'active' : ''} disabled={!lettersWithGames.has(l)} aria-pressed={letter === l}
              onClick={() => setLetter(letter === l ? '' : l)}>{l}</button>
          ))}
        </nav>

        <div className="library-results">
          {games.length === 0 ? (
            <div className="empty">
              <p>{t('library.empty')}</p>
              <button className="primary" onClick={() => setCreating(true)}>{t('library.addGame')}</button>
            </div>
          ) : filtered.length === 0 ? (
            <div className="empty"><p>{t('library.noResults')}</p></div>
          ) : view === 'posters' ? (
            <PosterGrid items={visible} grouping={grouping} onOpen={(i) => setOpen({ id: i.game.id, system: i.system })} onSystem={filterSystem} activeSystems={filters.systems} />
          ) : (
            <ul className="game-list">
              {visible.map((i) => <GameRow key={i.key} item={i} grouping={grouping} onOpen={() => setOpen({ id: i.game.id, system: i.system })}
                onSystem={filterSystem} activeSystems={filters.systems} />)}
            </ul>
          )}
          {visible.length < filtered.length && <div ref={sentinel} className="sentinel" aria-hidden="true" />}
        </div>
      </div>

      {open && <GameDetail gameId={open.id} system={open.system} onClose={() => setOpen(null)} onOpenGame={(id) => setOpen({ id, system: '' })} nav={nav}
        onPlatform={(p) => { setOpen(null); setFilters((f) => ({ ...f, platforms: [p] })); }} />}
      {creating && <NewGameDialog onClose={() => setCreating(false)} onCreated={(id) => { setCreating(false); setOpen({ id, system: '' }); }} />}
    </div>
  );
}

/** A library item as a list row: counts and the deadline come from its view (the edition's copies
 *  in "By platform"), the rest from the game. */
function GameRow({ item, grouping, onOpen, onSystem, activeSystems }: {
  item: LibraryItem<Game>;
  grouping: Grouping;
  onOpen: () => void;
  onSystem: (system: string) => void;
  activeSystems: string[];
}) {
  const { t } = useTranslation();
  const fmt = useFormatters();
  const game = item.game;
  const pending = item.view.copies.filter(isPendingKey).length;
  const redundant = item.view.copies.filter((c) => c.redundant).length;
  const deadline = nextDeadline(item.view);
  const days = daysUntil(deadline);
  const meta = [game.releaseYear || '', game.genres.slice(0, 3).join(', ')].filter(Boolean).join(' · ');
  return (
    <li>
      {/* The title button stretches over the row; the platform badges sit above it. */}
      <div className="game-row">
        <Cover game={game} system={item.system} className="row-cover" />
        <span className="row-main">
          <button className="row-open" onClick={onOpen}>{game.title}</button>
          {meta && <span className="row-meta">{meta}</span>}
          <ItemBadges item={item} grouping={grouping} onSystem={onSystem} activeSystems={activeSystems} />
        </span>
        <span className="row-side">
          {game.playStatus !== PlayStatus.UNSPECIFIED && <span className={`badge play-badge play-${playKey(game.playStatus)}`}>{t(`play.${playKey(game.playStatus)}`)}</span>}
          {game.rating > 0 && <Stars value={game.rating} />}
          {redundant > 0 && <span className="badge warn">{t('library.redundantBadge', { count: redundant })}</span>}
          {pending > 0 && redundant === 0 && <span className="badge">{t('library.pendingBadge', { count: pending })}</span>}
          {days !== null && (
            <span className={`row-deadline ${days <= EXPIRING_DAYS ? 'danger' : days <= 90 ? 'warn' : ''}`}>
              {fmt.date(deadline)} · {t('common.daysLeft', { count: days })}
            </span>
          )}
        </span>
      </div>
    </li>
  );
}

