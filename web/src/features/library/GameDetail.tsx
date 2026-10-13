import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, gameClient } from '../../api/client';
import { Alert } from '../../components/ui';
import { useEscape } from '../../components/useEscape';
import { openEdition } from '../../lib/editions';
import { gameInfo, type Game } from '../../lib/model';
import { useAppData } from '../../state/AppData';
import CopiesTab from './CopiesTab';
import EditTab from './EditTab';
import { SheetOverview, useGameDetails } from './GameSheet';
import SheetDialogs from './SheetDialogs';
import SheetHero from './SheetHero';
import type { GamePatch, SheetActions, SheetDialog, SheetNav } from './sheet';

interface Props {
  gameId: string;
  /** The edition to open on; empty or absent: the main one. */
  system?: string;
  onClose: () => void;
  /** Switch the dialog to another game (after a merge or move). */
  onOpenGame: (id: string) => void;
  /** The sheet now shows another edition of the game (a chip, a photo made its cover), so the
   *  library's ‹ › move from it. */
  onEdition?: (system: string) => void;
  /** Close the sheet and show the library filtered by that platform. */
  onPlatform?: (platform: string) => void;
  /** Move to the previous or next game of the list the sheet was opened from (← →, ‹ ›). Absent:
   *  no list (the buttons are hidden); a missing direction: the end of the list. */
  nav?: SheetNav;
}

type Tab = 'overview' | 'copies' | 'edit';

/** A game's sheet: details from the metadata providers, the copies you own, and editing. Renders
 *  nothing when the game is not in the catalog (deleted, or not loaded yet). */
export default function GameDetail({ gameId, ...props }: Props) {
  const { games } = useAppData();
  const game = games.find((g) => g.id === gameId);
  if (!game) return null;
  return <GameDetailBody game={game} {...props} />;
}

function GameDetailBody({ game, system, onClose, onOpenGame, onEdition, onPlatform, nav }: Omit<Props, 'gameId'> & { game: Game }) {
  const { t } = useTranslation();
  const { putGame, dropGame } = useAppData();
  const sheet = useGameDetails(game);
  const [tab, setTab] = useState<Tab>('overview');
  const [dialog, setDialog] = useState<SheetDialog | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [chosen, setChosen] = useState(system ?? '');
  // The edition shown: the one chosen, while the game still has it (its copies may have moved), else
  // the main one.
  const current = openEdition(game, chosen);
  // Follow the edition the library opens: ‹ › in "By platform" can move to another edition of the
  // same game, so the game's id alone is not enough.
  useEffect(() => setChosen(system ?? ''), [game.id, system]);
  const showEdition = (s: string) => {
    setChosen(s);
    onEdition?.(s);
  };

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError('');
    try {
      await fn();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const actions: SheetActions = { busy, run, openDialog: setDialog };

  const updateGame = (patch: GamePatch) => run(async () => {
    // Links are sent as set only by the Edit tab's save (a patch with links): any other save may come
    // from a page loaded before a link was added automatically, and must not remove it.
    putGame((await gameClient.updateGame({ ...gameInfo(game), ...patch, linksSet: 'links' in patch })).game!);
  });

  // Escape closes the sheet and ← → move to the previous / next game, only when no overlay is on top
  // of it: dialogs opened from the sheet or from inside a tab (the store-link search) register
  // above it on the Escape stack. The image viewer catches the arrows first (capture phase) and
  // keeps them; arrows typed in a field, or with a modifier (Alt+← is the browser's Back), are left
  // alone.
  const onTop = useEscape(onClose);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!onTop()) return;
      if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
      if ((e.target as HTMLElement | null)?.closest('input, textarea, select, [contenteditable="true"]')) return;
      const go = e.key === 'ArrowLeft' ? nav?.previous : e.key === 'ArrowRight' ? nav?.next : undefined;
      if (!go) return;
      e.preventDefault();
      go();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onTop, nav]);

  // Another game in the same sheet: drop what belonged to the previous one and start at the top.
  const overlay = useRef<HTMLDivElement>(null);
  const sheetEl = useRef<HTMLElement>(null);
  const shownId = useRef(game.id);
  useEffect(() => {
    if (shownId.current === game.id) return;
    shownId.current = game.id;
    setDialog(null);
    setError('');
    overlay.current?.scrollTo({ top: 0 });
    sheetEl.current?.scrollTo({ top: 0 });
  }, [game.id]);
  useEffect(() => {
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => { document.body.style.overflow = prev; };
  }, []);

  return (
    <div ref={overlay} className="overlay sheet-overlay" onMouseDown={onClose}>
      <article ref={sheetEl} className="game-sheet" role="dialog" aria-modal="true" aria-labelledby="sheet-title" onMouseDown={(e) => e.stopPropagation()}>
        <SheetHero game={game} current={current} details={sheet.details} nav={nav} actions={actions} onClose={onClose}
          onPlatform={onPlatform} onShowEdition={showEdition} onChange={updateGame} />

        <nav className="sheet-tabs" role="tablist">
          {(['overview', 'copies', 'edit'] as Tab[]).map((k) => (
            <button key={k} role="tab" aria-selected={tab === k} className={tab === k ? 'active' : ''} onClick={() => setTab(k)}>
              {k === 'copies' ? t('game.copies', { count: game.copies.length }) : t(`details.tabs.${k}`)}
            </button>
          ))}
        </nav>

        {/* Keyed by game: each game's tab content (forms, viewer) starts fresh; the tab itself stays. */}
        <div className="sheet-body" key={game.id}>
          {error && <Alert tone="error">{error}</Alert>}
          {tab === 'overview' && (
            <SheetOverview game={game} details={sheet.details} warnings={sheet.warnings} loading={sheet.loading} error={sheet.error} onRefresh={sheet.refresh} />
          )}
          {tab === 'copies' && (
            <CopiesTab game={game} current={current} actions={actions} onShowEdition={showEdition}
              onGameGone={() => { dropGame(game.id); onClose(); }} />
          )}
          {tab === 'edit' && <EditTab game={game} actions={actions} onSave={updateGame} onDeleted={onClose} />}
        </div>
      </article>

      <SheetDialogs game={game} current={current} dialog={dialog} run={run} onClose={() => setDialog(null)} onOpenGame={onOpenGame} />
    </div>
  );
}
