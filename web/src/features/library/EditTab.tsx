import { useEffect, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { gameClient } from '../../api/client';
import type { LinkStore } from '../../gen/gamevault/v1/game_pb';
import { cleanLinks, isAutoLink, rebaseInfo, sameLinks, type GameInfo } from '../../lib/links';
import type { Game } from '../../lib/model';
import { useAppData } from '../../state/AppData';
import { FieldInputs, applyField, cleanFields, sameFields, useFieldValidity } from '../fields/FieldInput';
import LinkSearchDialog from './LinkSearchDialog';
import type { GamePatch, SheetActions } from './sheet';

type Info = GameInfo<Game['fields'][string]>;

const infoOf = (g: Game): Info => ({ title: g.title, links: { ...g.links }, notes: g.notes, fields: { ...g.fields } });

/** Two field values are the same when they save the same. */
const sameField = (a: Game['fields'][string], b: Game['fields'][string]) => sameFields({ v: a }, { v: b });

/** The sheet's "Edit" tab: title, store links, notes and the game's custom fields; merge and delete. */
export default function EditTab({ game, actions, onSave, onDeleted }: {
  game: Game;
  actions: SheetActions;
  onSave: (patch: GamePatch) => Promise<void>;
  onDeleted: () => void;
}) {
  const { t } = useTranslation();
  const { dropGame, putGame, fields } = useAppData();
  const { busy, run, openDialog } = actions;
  const gameDefs = fields.filter((f) => f.scope === 'game');
  const [info, setInfo] = useState(() => infoOf(game));
  // The game as the form last took it: telling its info apart from the form's says what the user
  // changed.
  const [baseGame, setBaseGame] = useState(game);
  const base = infoOf(baseGame);
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

  // The game as it is now, so the links shown include any found automatically since the page loaded.
  useEffect(() => {
    // Only when the tab opens for a game (putGame never changes): later changes arrive through the
    // game itself.
    gameClient.getGame({ id: game.id }).then((res) => { if (res.game) putGame(res.game); }, () => {});
  }, [game.id, putGame]);

  // When the game changes while the tab is open (reloaded above, a link found automatically, this
  // form's own save), the form takes the new values and keeps only what the user changed in it, so
  // neither their typing nor the new links are lost. Adjusted during render, not in an effect, so
  // the form never renders once with the old base.
  if (game !== baseGame) {
    setBaseGame(game);
    setInfo((x) => rebaseInfo(base, x, infoOf(game), sameField));
  }

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

  const save = async (e: FormEvent) => {
    e.preventDefault();
    if (fieldsInvalid) return;
    // Links found automatically since the game was last read are kept: only the user's edits apply.
    let current = game;
    try {
      current = (await gameClient.getGame({ id: game.id })).game ?? game;
    } catch {
      // Saving on what the page has is still right for every link the user did not touch.
    }
    const edited = rebaseInfo(base, info, infoOf(current), sameField);
    await onSave({ ...edited, links: cleanLinks(edited.links), fields: cleanFields(edited.fields) });
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
                <span>
                  {store.name}
                  {isAutoLink(game, store.key, info.links[store.key]) && (
                    <span className="badge auto-link" title={t('game.autoLinkHelp')}>{t('game.autoLink')}</span>
                  )}
                </span>
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
        <button onClick={() => openDialog({ type: 'merge' })} disabled={busy} title={t('game.mergeHelp')}>{t('game.merge')}</button>
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
