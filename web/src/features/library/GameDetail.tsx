import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, gameClient } from '../../api/client';
import { Cover } from '../../components/Cover';
import { Icon } from '../../components/Icon';
import { PlatformBadge, platformHoldings } from '../../components/PlatformBadge';
import { Alert, KeyCell, useFormatters } from '../../components/ui';
import { mainSystem } from '../../lib/editions';
import { formatAmount } from '../../lib/money';
import {
  CopyKind, CopyStatus, contentKey, daysUntil, gameInfo, gradeKey, kindKey, redeemUrl, statusKey, type Copy, type CopyDetailsInput, type Game,
} from '../../lib/model';
import type { LinkStore } from '../../gen/gamevault/v1/game_pb';
import { useAppData } from '../../state/AppData';
import { FieldInputs, applyField, cleanFields, sameFields, useFieldValidity } from '../fields/FieldInput';
import { FieldList } from '../fields/FieldList';
import CopyForm from './CopyForm';
import CopyPhotos from './CopyPhotos';
import CopyValue from './CopyValue';
import CoverPicker from './CoverPicker';
import GamePicker from './GamePicker';
import { SheetFacts, SheetOverview, useGameDetails } from './GameSheet';
import LinkSearchDialog from './LinkSearchDialog';
import PlayControls from './PlayControls';

interface Props {
  gameId: string;
  /** The edition to open on; empty: the main one. */
  system?: string;
  onClose: () => void;
  /** Switch the dialog to another game (after a merge or move). */
  onOpenGame: (id: string) => void;
  /** Close the sheet and show the library filtered by that platform. */
  onPlatform?: (platform: string) => void;
  /** Move to the previous or next game of the list the sheet was opened from (← →, ‹ ›). Absent:
   *  no list (the buttons are hidden); a missing direction: the end of the list. */
  nav?: SheetNav;
}

/** Moves between the games of the list a sheet was opened from. */
export interface SheetNav {
  previous?: () => void;
  next?: () => void;
}

type Dialog =
  | { type: 'addCopy' }
  | { type: 'editCopy'; copy: Copy }
  | { type: 'moveCopy'; copy: Copy }
  | { type: 'merge' }
  | { type: 'coverPicker' }
  | null;

type Tab = 'overview' | 'copies' | 'edit';

/** The editable fields a change sends; the rest are sent back as they are. */
type GamePatch = Partial<Pick<Game, 'title' | 'links' | 'notes' | 'playStatus' | 'rating' | 'fields'>>;

/** A game's sheet (like CLZ): details from the metadata providers, the copies you own, and editing. */
export default function GameDetail({ gameId, onClose, onOpenGame, onPlatform, nav }: Props) {
  const { games } = useAppData();
  const game = games.find((g) => g.id === gameId);
  if (!game) return null;
  return <GameDetailBody game={game} onClose={onClose} onOpenGame={onOpenGame} onPlatform={onPlatform} nav={nav} />;
}

function GameDetailBody({ game, onClose, onOpenGame, onPlatform, nav }: {
  game: Game;
  onClose: () => void;
  onOpenGame: (id: string) => void;
  onPlatform?: (platform: string) => void;
  nav?: SheetNav;
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

  const updateGame = (patch: GamePatch) => run(async () => {
    putGame((await gameClient.updateGame({ ...gameInfo(game), ...patch })).game!);
  });

  const details = sheet.details;
  const release = details?.releaseDate.match(/\b(19|20)\d\d\b/)?.[0] ?? (game.releaseYear || '');
  const genres = details?.genres.length ? details.genres : game.genres;

  // Escape closes the sheet and ← → move to the previous / next game, only when no nested dialog is
  // on top of it. The image viewer catches the arrows first (capture phase) and keeps them; arrows
  // typed in a field, or with a modifier (Alt+← is the browser's Back), are left alone.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (dialog) return;
      if (e.key === 'Escape') {
        onClose();
        return;
      }
      if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
      if ((e.target as HTMLElement | null)?.closest('input, textarea, select, [contenteditable="true"]')) return;
      const go = e.key === 'ArrowLeft' ? nav?.previous : e.key === 'ArrowRight' ? nav?.next : undefined;
      if (!go) return;
      e.preventDefault();
      go();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [dialog, onClose, nav]);

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
        <header className="hero">
          <div className="hero-backdrop" aria-hidden="true"><Cover game={game} /></div>
          <div className="hero-actions">
            {nav && (
              <>
                <button className="icon-button" onClick={nav.previous} disabled={!nav.previous} aria-label={t('game.previousGame')} title={t('game.previousGameKey')}><Icon name="prev" size={20} /></button>
                <button className="icon-button" onClick={nav.next} disabled={!nav.next} aria-label={t('game.nextGame')} title={t('game.nextGameKey')}><Icon name="next" size={20} /></button>
              </>
            )}
            <button className="icon-button" onClick={onClose} aria-label={t('common.close')}><Icon name="close" size={20} /></button>
          </div>
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
            <PlayControls game={game} busy={busy} onChange={updateGame} />
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

        {/* Keyed by game: each game's tab content (forms, viewer) starts fresh; the tab itself stays. */}
        <div className="sheet-body" key={game.id}>
          {error && <Alert tone="error">{error}</Alert>}
          {tab === 'overview' && (
            <SheetOverview game={game} details={details} warnings={sheet.warnings} loading={sheet.loading} error={sheet.error} onRefresh={sheet.refresh} />
          )}
          {tab === 'copies' && <CopiesTab game={game} busy={busy} run={run} setDialog={setDialog} onGameGone={() => { dropGame(game.id); onClose(); }} />}
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
      {/* The main edition's cover for now; a game without copies has its main edition on ''. */}
      {dialog?.type === 'coverPicker' && (
        <CoverPicker game={game} system={mainSystem(game)} onClose={() => setDialog(null)}
          onPick={(cover) => {
            run(async () => putGame((await gameClient.setEditionCover({ gameId: game.id, system: mainSystem(game), cover })).game!));
            setDialog(null);
          }} />
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

function CopiesTab({ game, busy, run, setDialog, onGameGone }: {
  game: Game;
  busy: boolean;
  run: (fn: () => Promise<void>) => Promise<void>;
  setDialog: (d: Dialog) => void;
  /** The game was deleted (its last copy was removed for good). */
  onGameGone: () => void;
}) {
  const { t, i18n } = useTranslation();
  const fmt = useFormatters();
  const { putGame, sourceName, reloadSources, fields } = useAppData();
  const copyDefs = fields.filter((f) => f.scope === 'copy');
  // Copy whose key was just opened in the store: offer to mark it as redeemed.
  const [redeeming, setRedeeming] = useState<string | null>(null);

  const markRedeemed = (c: Copy) => run(async () => {
    const res = await gameClient.updateCopy({ gameId: game.id, copyId: c.id, details: { ...c.details!, status: CopyStatus.REDEEMED } });
    putGame(res.game!);
    setRedeeming(null);
  });

  const deleteCopy = (c: Copy) => {
    if (!c.sourceId || !c.externalId) {
      if (!confirm(t('copy.confirmDelete'))) return;
      run(async () => putGame((await gameClient.deleteCopy({ gameId: game.id, copyId: c.id })).game!));
      return;
    }
    // An imported copy would come back on the next sync: remove it for good instead.
    const loses = c.photos.length > 0 || c.estimates.length > 0;
    const question = t('copy.confirmExclude', { title: game.title, source: sourceName(c.sourceId) })
      + (loses ? t('copy.confirmExcludeLoses') : '');
    if (!confirm(question)) return;
    run(async () => {
      const res = await gameClient.excludeCopy({ gameId: game.id, copyId: c.id });
      if (res.game) putGame(res.game);
      else onGameGone();
      // The source's removed list changed; the page does not wait for it.
      void reloadSources();
    });
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
          const physical = [
            d.grade ? t(`grade.${gradeKey(d.grade)}`) : '',
            d.contents.length ? d.contents.map((c) => t(`content.${contentKey(c)}`)).join(', ') : '',
            d.price?.amountMinor ? formatAmount(d.price.amountMinor, d.price.currency, i18n.language) : '',
            d.location,
          ];
          const extra = [d.origin, d.edition, ...physical, fmt.date(d.acquiredOn), d.barcode && `EAN ${d.barcode}`].filter(Boolean);
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
              {d.kind === CopyKind.PHYSICAL && <CopyValue game={game} copy={c} onAddBarcode={() => setDialog({ type: 'editCopy', copy: c })} />}
              <FieldList className="copy-fields" values={d.fields}
                defs={copyDefs.filter((f) => !f.kinds.length || f.kinds.includes(d.kind))} />
              {d.notes && <p className="small">{d.notes}</p>}
              <CopyPhotos game={game} copy={c} />
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
  onSave: (patch: GamePatch) => Promise<void>;
  setDialog: (d: Dialog) => void;
  run: (fn: () => Promise<void>) => Promise<void>;
  onDeleted: () => void;
}) {
  const { t } = useTranslation();
  const { dropGame, fields } = useAppData();
  const gameDefs = fields.filter((f) => f.scope === 'game');
  const infoOf = (g: Game) => ({ title: g.title, links: { ...g.links }, notes: g.notes, fields: { ...g.fields } });
  const [info, setInfo] = useState(() => infoOf(game));
  // Discard remounts the field inputs, so text they kept locally (an invalid number) goes too.
  const [discarded, setDiscarded] = useState(0);
  // A field holding invalid text (a number, a year) blocks Save until it is fixed or cleared.
  const [fieldsInvalid, reportField] = useFieldValidity();
  const [stores, setStores] = useState<LinkStore[]>([]);
  const [searching, setSearching] = useState<LinkStore | null>(null);
  const dirty = info.title !== game.title || !sameLinks(info.links, game.links) || info.notes !== game.notes
    || !sameFields(info.fields, game.fields);

  useEffect(() => {
    gameClient.listLinkStores({}).then((res) => setStores(res.stores), () => setStores([]));
  }, []);

  // One row per store the game is linked to, plus the searchable ones (to link it by title), in the
  // server's order; a link to a store nobody describes any more keeps a row named by its key.
  const shown = (key: string) => key in info.links || stores.some((s) => s.key === key && s.searchable);
  const rows = [
    ...stores.filter((s) => shown(s.key)),
    ...Object.keys(info.links).sort()
      .filter((key) => !stores.some((s) => s.key === key))
      .map((key) => ({ key, name: key, pageUrl: '', searchable: false }) as LinkStore),
  ];
  const addable = stores.filter((s) => !shown(s.key));
  const setLink = (key: string, id: string) => setInfo({ ...info, links: { ...info.links, [key]: id } });

  const save = (e: FormEvent) => {
    e.preventDefault();
    if (fieldsInvalid) return;
    onSave({ ...info, links: cleanLinks(info.links), fields: cleanFields(info.fields) });
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
                  {store.searchable && (
                    <button type="button" onClick={() => setSearching(store)}>{t('game.searchStore')}</button>
                  )}
                </span>
              </label>
            );
          })}
          {addable.length > 0 && (
            <select className="store-link-add" value="" aria-label={t('game.addLink')}
              onChange={(e) => e.target.value && setLink(e.target.value, '')}>
              <option value="">{t('game.addLink')}</option>
              {addable.map((s) => <option key={s.key} value={s.key}>{s.name}</option>)}
            </select>
          )}
          <span className="help">{t('game.linksHelp')}</span>
        </div>
        <label className="span2">
          {t('common.notes')}
          <textarea rows={3} value={info.notes} onChange={(e) => setInfo({ ...info, notes: e.target.value })} />
        </label>
        <FieldInputs key={discarded} className="span2" defs={gameDefs} values={info.fields} onValidity={reportField}
          onChange={(id, update) => setInfo((x) => ({ ...x, fields: applyField(x.fields, id, update) }))} />
        <div className="span2 actions">
          <span className="spacer" />
          {dirty && <button type="button" onClick={() => { setInfo(infoOf(game)); setDiscarded((n) => n + 1); }}>{t('common.discard')}</button>}
          <button type="submit" className="primary" disabled={busy || !dirty || fieldsInvalid}>{t('common.save')}</button>
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
