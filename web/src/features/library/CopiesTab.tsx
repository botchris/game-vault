import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { gameClient } from '../../api/client';
import { Icon } from '../../components/Icon';
import { PlatformBadge } from '../../components/PlatformBadge';
import { groupCopies } from '../../lib/editions';
import { CopyStatus, type Copy, type Game } from '../../lib/model';
import { useAppData } from '../../state/AppData';
import CopyCard from './CopyCard';
import type { SheetActions } from './sheet';

/** The sheet's "Copies" tab: the game's copies grouped by edition, the one shown first. */
export default function CopiesTab({ game, current, actions, onShowEdition, onGameGone }: {
  game: Game;
  /** The edition the sheet shows: its copies come first. */
  current: string;
  actions: SheetActions;
  /** Show this edition in the sheet (a photo just became its cover). */
  onShowEdition: (system: string) => void;
  /** The game was deleted (its last copy was removed for good). */
  onGameGone: () => void;
}) {
  const { t } = useTranslation();
  const { putGame, sourceName, reloadSources } = useAppData();
  const { run, openDialog } = actions;
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
        <button className="primary" onClick={() => openDialog({ type: 'addCopy' })}><Icon name="plus" size={18} />{t('copy.add')}</button>
      </div>
      {game.copies.length === 0 && <p className="muted">{t('game.noCopies')}</p>}
      {groupCopies(game.copies, game.editions, current).map((group) => (
        <section key={group.system} className="copies-group">
          {game.editions.length >= 2 && <h3 className="copies-system"><PlatformBadge platform={group.system} full /></h3>}
          <ul className="copy-cards">
            {group.copies.map((c) => (
              <CopyCard key={c.id} game={game} copy={c} actions={actions} redeeming={redeeming === c.id}
                onRedeeming={() => setRedeeming(c.id)} onMarkRedeemed={() => markRedeemed(c)} onDelete={() => deleteCopy(c)}
                onShowEdition={onShowEdition} />
            ))}
          </ul>
        </section>
      ))}
    </>
  );
}
