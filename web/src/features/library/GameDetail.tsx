import { useEffect, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, gameClient } from '../../api/client';
import { Cover } from '../../components/Cover';
import { Icon } from '../../components/Icon';
import { PlatformBadge, platformHoldings } from '../../components/PlatformBadge';
import { Alert, KeyCell, useFormatters } from '../../components/ui';
import { CopyKind, CopyStatus, daysUntil, kindKey, redeemUrl, statusKey, type Copy, type CopyDetailsInput, type Game } from '../../lib/model';
import type { LinkStore } from '../../gen/gamevault/v1/game_pb';
import { useAppData } from '../../state/AppData';
import CopyForm from './CopyForm';
import CoverPicker from './CoverPicker';
import GamePicker from './GamePicker';
import { SheetFacts, SheetOverview, useGameDetails } from './GameSheet';
import LinkSearchDialog from './LinkSearchDialog';

interface Props {
  gameId: string;
  onClose: () => void;
  /** Switch the dialog to another game (after a merge or move). */
  onOpenGame: (id: string) => void;
  /** Close the sheet and show the library filtered by that platform. */
  onPlatform?: (platform: string) => void;
}

type Dialog =
  | { type: 'addCopy' }
  | { type: 'editCopy'; copy: Copy }
  | { type: 'moveCopy'; copy: Copy }
  | { type: 'merge' }
  | { type: 'coverPicker' }
  | null;

type Tab = 'overview' | 'copies' | 'edit';

/** A game's sheet (like CLZ): details from the metadata providers, the copies you own, and editing. */
export default function GameDetail({ gameId, onClose, onOpenGame, onPlatform }: Props) {
  const { games } = useAppData();
  const game = games.find((g) => g.id === gameId);
  if (!game) return null;
  return <GameDetailBody game={game} onClose={onClose} onOpenGame={onOpenGame} onPlatform={onPlatform} />;
}

function GameDetailBody({ game, onClose, onOpenGame, onPlatform }: {
  game: Game;
  onClose: () => void;
  onOpenGame: (id: string) => void;
  onPlatform?: (platform: string) => void;
}) {
  const { t } = useTranslation();
  const { games, putGame, dropGame } = useAppData();
  const sheet = useGameDetails(game);
  const [tab, setTab] = useState<Tab>('overview');
  const [dialog, setDialog] = useState<Dialog>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

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

  const updateGame = (patch: Partial<Pick<Game, 'title' | 'links' | 'notes' | 'coverUrl'>>) => run(async () => {
    const res = await gameClient.updateGame({
      id: game.id, title: game.title, links: game.links, notes: game.notes, coverUrl: game.coverUrl, ...patch,
    });
    putGame(res.game!);
  });

  const details = sheet.details;
  const release = details?.releaseDate.match(/\b(19|20)\d\d\b/)?.[0] ?? (game.releaseYear || '');
  const genres = details?.genres.length ? details.genres : game.genres;

  // Escape closes the sheet only when no nested dialog is on top of it.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !dialog && onClose();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [dialog, onClose]);
  useEffect(() => {
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => { document.body.style.overflow = prev; };
  }, []);

  return (
    <div className="overlay sheet-overlay" onMouseDown={onClose}>
      <article className="game-sheet" role="dialog" aria-modal="true" aria-labelledby="sheet-title" onMouseDown={(e) => e.stopPropagation()}>
        <header className="hero">
          <div className="hero-backdrop" aria-hidden="true"><Cover game={game} /></div>
          <button className="icon-button hero-close" onClick={onClose} aria-label={t('common.close')}><Icon name="close" size={20} /></button>
          <div className="hero-cover">
            <Cover game={game} />
            <button type="button" className="ghost small-button" onClick={() => setDialog({ type: 'coverPicker' })}>
              <Icon name="image" size={16} />{t('coverPicker.open')}
            </button>
          </div>
          <div className="hero-body">
            <h2 id="sheet-title" className="hero-title">{game.title}</h2>
            <div className="hero-line">
              {release && <span className="hero-year">{release}</span>}
              {platformHoldings(game).map((h) => <PlatformBadge key={h.platform} {...h} full onSelect={onPlatform} />)}
            </div>
            {genres.length > 0 && <div className="genres">{genres.map((g) => <span key={g} className="genre">{g}</span>)}</div>}
            <SheetFacts details={details} />
          </div>
        </header>

        <nav className="sheet-tabs" role="tablist">
          {(['overview', 'copies', 'edit'] as Tab[]).map((k) => (
            <button key={k} role="tab" aria-selected={tab === k} className={tab === k ? 'active' : ''} onClick={() => setTab(k)}>
              {k === 'copies' ? t('game.copies', { count: game.copies.length }) : t(`details.tabs.${k}`)}
            </button>
          ))}
        </nav>

        <div className="sheet-body">
          {error && <Alert tone="error">{error}</Alert>}
          {tab === 'overview' && (
            <SheetOverview game={game} details={details} warnings={sheet.warnings} loading={sheet.loading} error={sheet.error} onRefresh={sheet.refresh} />
          )}
          {tab === 'copies' && <CopiesTab game={game} busy={busy} run={run} setDialog={setDialog} />}
          {tab === 'edit' && <EditTab game={game} busy={busy} onSave={updateGame} setDialog={setDialog} run={run} onDeleted={onClose} />}
        </div>
      </article>

      {dialog?.type === 'addCopy' && (
        <CopyForm onClose={() => setDialog(null)} onSubmit={async (d) => {
          putGame((await gameClient.addCopy({ gameId: game.id, details: d })).game!);
          setDialog(null);
        }} />
      )}
      {dialog?.type === 'editCopy' && <EditCopyDialog game={game} copy={dialog.copy} onClose={() => setDialog(null)} />}
      {dialog?.type === 'moveCopy' && (
        <GamePicker title={t('copy.moveTitle')} excludeId={game.id} allowNew onClose={() => setDialog(null)}
          onPick={(target) => run(async () => {
            const res = await gameClient.moveCopy({
              gameId: game.id, copyId: dialog.copy.id,
              targetGameId: 'id' in target ? target.id : '', newGameTitle: 'newTitle' in target ? target.newTitle : '',
            });
            putGame(res.targetGame!);
            if (res.sourceGame) putGame(res.sourceGame);
            else { dropGame(game.id); onOpenGame(res.targetGame!.id); }
            setDialog(null);
          })} />
      )}
      {dialog?.type === 'coverPicker' && (
        <CoverPicker gameId={game.id} current={game.coverUrl} onClose={() => setDialog(null)}
          onPick={(url) => { updateGame({ coverUrl: url }); setDialog(null); }} />
      )}
      {dialog?.type === 'merge' && (
        <GamePicker title={t('game.mergeTitle', { title: game.title })} excludeId={game.id} onClose={() => setDialog(null)}
          onPick={(target) => {
            if (!('id' in target)) return;
            const other = games.find((g) => g.id === target.id);
            if (!confirm(t('game.confirmMerge', { other: other?.title, title: game.title }))) return;
            run(async () => {
              const res = await gameClient.mergeGames({ targetId: game.id, sourceIds: [target.id] });
              dropGame(target.id);
              putGame(res.game!);
              setDialog(null);
            });
          }} />
      )}
    </div>
  );
}

function EditCopyDialog({ game, copy, onClose }: { game: Game; copy: Copy; onClose: () => void }) {
  const { putGame, sourceName } = useAppData();
  return (
    <CopyForm initial={{ ...copy.details! }} managedBy={copy.sourceId ? sourceName(copy.sourceId) : undefined} onClose={onClose}
      onSubmit={async (d: CopyDetailsInput) => {
        putGame((await gameClient.updateCopy({ gameId: game.id, copyId: copy.id, details: d })).game!);
        onClose();
      }} />
  );
}

function CopiesTab({ game, busy, run, setDialog }: {
  game: Game;
  busy: boolean;
  run: (fn: () => Promise<void>) => Promise<void>;
  setDialog: (d: Dialog) => void;
}) {
  const { t } = useTranslation();
  const fmt = useFormatters();
  const { putGame, sourceName } = useAppData();
  // Copy whose key was just opened in the store: offer to mark it as redeemed.
  const [redeeming, setRedeeming] = useState<string | null>(null);

  const markRedeemed = (c: Copy) => run(async () => {
    const res = await gameClient.updateCopy({ gameId: game.id, copyId: c.id, details: { ...c.details!, status: CopyStatus.REDEEMED } });
    putGame(res.game!);
    setRedeeming(null);
  });

  const deleteCopy = (c: Copy) => {
    if (!confirm(t('copy.confirmDelete'))) return;
    run(async () => putGame((await gameClient.deleteCopy({ gameId: game.id, copyId: c.id })).game!));
  };

  return (
    <>
      <div className="section-head">
        <span />
        <button className="primary" onClick={() => setDialog({ type: 'addCopy' })}><Icon name="plus" size={18} />{t('copy.add')}</button>
      </div>
      {game.copies.length === 0 && <p className="muted">{t('game.noCopies')}</p>}
      <ul className="copy-cards">
        {game.copies.map((c) => {
          const d = c.details!;
          const days = daysUntil(d.redeemBy);
          const source = c.sourceId ? sourceName(c.sourceId) : '';
          const extra = [d.origin, d.edition, d.condition, d.location, fmt.date(d.acquiredOn), d.barcode && `EAN ${d.barcode}`].filter(Boolean);
          return (
            <li key={c.id} className={`copy-card kind-edge-${kindKey(d.kind)}`}>
              <div className="copy-top">
                <span className={`kind kind-${kindKey(d.kind)}`}>{t(`kind.${kindKey(d.kind)}`)}</span>
                {d.platform && <PlatformBadge platform={d.platform} full />}
                <span className={`status status-${statusKey(d.status)}`}>{t(`status.${statusKey(d.status)}`)}</span>
                {c.redundant && <span className="badge warn" title={t('copy.redundantHelp')}>{t('copy.redundant')}</span>}
                <span className="spacer" />
                <span className="copy-actions">
                  <button className="icon-button" title={t('common.edit')} aria-label={t('common.edit')} onClick={() => setDialog({ type: 'editCopy', copy: c })}><Icon name="edit" size={18} /></button>
                  <button className="icon-button" title={t('copy.move')} aria-label={t('copy.move')} onClick={() => setDialog({ type: 'moveCopy', copy: c })}><Icon name="move" size={18} /></button>
                  <button className="icon-button" title={t('common.delete')} aria-label={t('common.delete')} onClick={() => deleteCopy(c)}><Icon name="trash" size={18} /></button>
                </span>
              </div>
              {d.kind === CopyKind.KEY && (
                <div className="copy-key">
                  <KeyCell value={d.key} />
                  {redeemUrl(c) && (
                    <a className="button small-button" href={redeemUrl(c)!} target="_blank" rel="noreferrer" onClick={() => setRedeeming(c.id)}>
                      {t('copy.redeemOn', { platform: d.platform })}<Icon name="external" size={14} />
                    </a>
                  )}
                  {redeeming === c.id && (
                    <button className="small-button primary" onClick={() => markRedeemed(c)} disabled={busy}><Icon name="check" size={14} />{t('copy.markRedeemed')}</button>
                  )}
                </div>
              )}
              {days !== null && (
                <p className={`copy-deadline ${days <= 30 ? 'danger' : days <= 90 ? 'warn' : 'muted'}`}>
                  {t('copy.redeemBy')}: {fmt.date(d.redeemBy)} · {t('common.daysLeft', { count: days })}
                </p>
              )}
              {extra.length > 0 && <p className="muted small">{extra.join(' · ')}</p>}
              {d.notes && <p className="small">{d.notes}</p>}
              {source && <p className="muted small">{t('copy.syncedFrom', { source })}</p>}
            </li>
          );
        })}
      </ul>
    </>
  );
}

function EditTab({ game, busy, onSave, setDialog, run, onDeleted }: {
  game: Game;
  busy: boolean;
  onSave: (patch: Partial<Pick<Game, 'title' | 'links' | 'notes' | 'coverUrl'>>) => Promise<void>;
  setDialog: (d: Dialog) => void;
  run: (fn: () => Promise<void>) => Promise<void>;
  onDeleted: () => void;
}) {
  const { t } = useTranslation();
  const { dropGame } = useAppData();
  const infoOf = (g: Game) => ({ title: g.title, links: { ...g.links }, notes: g.notes, coverUrl: g.coverUrl });
  const [info, setInfo] = useState(() => infoOf(game));
  const [stores, setStores] = useState<LinkStore[]>([]);
  const [searching, setSearching] = useState<LinkStore | null>(null);
  const dirty = info.title !== game.title || !sameLinks(info.links, game.links) || info.notes !== game.notes || info.coverUrl !== game.coverUrl;

  useEffect(() => {
    gameClient.listLinkStores({}).then((res) => setStores(res.stores), () => setStores([]));
  }, []);

  // Searchable stores first, in chain order, then any other store the game is linked to (from an import).
  const rows = [
    ...stores,
    ...Object.keys(info.links).sort()
      .filter((key) => !stores.some((s) => s.key === key))
      .map((key) => ({ key, name: key, pageUrl: '' }) as LinkStore),
  ];
  const setLink = (key: string, id: string) => setInfo({ ...info, links: { ...info.links, [key]: id } });

  const save = (e: FormEvent) => {
    e.preventDefault();
    onSave({ ...info, links: cleanLinks(info.links) });
  };

  const deleteGame = () => {
    if (!confirm(t('game.confirmDelete', { title: game.title, count: game.copies.length }))) return;
    run(async () => {
      await gameClient.deleteGame({ id: game.id });
      dropGame(game.id);
      onDeleted();
    });
  };

  return (
    <>
      <form className="grid" onSubmit={save}>
        <label className="span2">
          {t('game.title')}
          <input value={info.title} onChange={(e) => setInfo({ ...info, title: e.target.value })} required />
        </label>
        <div className="span2 store-links">
          <span className="store-links-title">{t('game.links')}</span>
          {rows.map((store) => {
            const id = (info.links[store.key] ?? '').trim();
            return (
              <label key={store.key} className="store-link">
                <span>{store.name}</span>
                <span className="row tight">
                  <input value={info.links[store.key] ?? ''} placeholder={t('game.linkPlaceholder')} spellCheck={false}
                    onChange={(e) => setLink(store.key, e.target.value)} />
                  {store.pageUrl && id && (
                    <a className="button" href={store.pageUrl.replace('{id}', encodeURIComponent(id))} target="_blank" rel="noreferrer">
                      {t('game.storePage')}
                    </a>
                  )}
                  {stores.includes(store) && (
                    <button type="button" onClick={() => setSearching(store)}>{t('game.searchStore')}</button>
                  )}
                </span>
              </label>
            );
          })}
          <span className="help">{t('game.linksHelp')}</span>
        </div>
        <label className="span2">
          {t('game.coverUrl')}
          <input type="url" value={info.coverUrl} placeholder={t('game.coverUrlPlaceholder')}
            onChange={(e) => setInfo({ ...info, coverUrl: e.target.value })} />
        </label>
        <label className="span2">
          {t('common.notes')}
          <textarea rows={3} value={info.notes} onChange={(e) => setInfo({ ...info, notes: e.target.value })} />
        </label>
        <div className="span2 actions">
          <span className="spacer" />
          {dirty && <button type="button" onClick={() => setInfo(infoOf(game))}>{t('common.discard')}</button>}
          <button type="submit" className="primary" disabled={busy || !dirty}>{t('common.save')}</button>
        </div>
      </form>
      <div className="actions danger-zone">
        <button onClick={() => setDialog({ type: 'merge' })} disabled={busy} title={t('game.mergeHelp')}>{t('game.merge')}</button>
        <span className="spacer" />
        <button className="danger" onClick={deleteGame} disabled={busy}>{t('game.delete')}</button>
      </div>
      {searching && (
        <LinkSearchDialog store={searching} initialQuery={info.title} onClose={() => setSearching(null)}
          onPick={(match) => { setLink(searching.key, match.id); setSearching(null); }} />
      )}
    </>
  );
}

/** Links without the cleared stores (an empty id unlinks). */
function cleanLinks(links: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.entries(links).map(([k, v]) => [k, v.trim()]).filter(([, v]) => v !== ''));
}

function sameLinks(a: Record<string, string>, b: Record<string, string>): boolean {
  const ca = cleanLinks(a), cb = cleanLinks(b);
  const keys = Object.keys(ca);
  return keys.length === Object.keys(cb).length && keys.every((k) => ca[k] === cb[k]);
}
