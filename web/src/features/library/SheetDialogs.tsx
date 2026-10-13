import { useTranslation } from 'react-i18next';
import { gameClient } from '../../api/client';
import type { Game } from '../../lib/model';
import { useAppData } from '../../state/AppData';
import CopyForm from './CopyForm';
import CoverPicker from './CoverPicker';
import EditCopyDialog from './EditCopyDialog';
import GamePicker from './GamePicker';
import type { SheetActions, SheetDialog } from './sheet';

/** The dialog the sheet has open over it, if any: add, edit or move a copy, pick a cover, merge. */
export default function SheetDialogs({ game, current, dialog, run, onClose, onOpenGame }: {
  game: Game;
  /** The edition the sheet shows: the cover picker sets its cover. */
  current: string;
  dialog: SheetDialog | null;
  run: SheetActions['run'];
  onClose: () => void;
  /** Switch the sheet to another game (the copy moved was the game's last one). */
  onOpenGame: (id: string) => void;
}) {
  const { t } = useTranslation();
  const { games, putGame, dropGame } = useAppData();
  switch (dialog?.type) {
    case 'addCopy':
      return (
        <CopyForm onClose={onClose} onSubmit={async (d) => {
          putGame((await gameClient.addCopy({ gameId: game.id, details: d })).game!);
          onClose();
        }} />
      );
    case 'editCopy':
      return <EditCopyDialog game={game} copy={dialog.copy} onClose={onClose} />;
    case 'moveCopy':
      return (
        <GamePicker title={t('copy.moveTitle')} excludeId={game.id} allowNew onClose={onClose}
          onPick={(target) => run(async () => {
            const res = await gameClient.moveCopy({
              gameId: game.id, copyId: dialog.copy.id,
              targetGameId: 'id' in target ? target.id : '', newGameTitle: 'newTitle' in target ? target.newTitle : '',
            });
            putGame(res.targetGame!);
            if (res.sourceGame) putGame(res.sourceGame);
            else { dropGame(game.id); onOpenGame(res.targetGame!.id); }
            onClose();
          })} />
      );
    // The current edition's cover; a game without copies has its only edition on ''. A URL the
    // server refuses comes back as the sheet's error.
    case 'coverPicker':
      return (
        <CoverPicker game={game} system={current} photosOnly={dialog.photosOnly} onClose={onClose}
          onPick={(cover) => {
            run(async () => putGame((await gameClient.setEditionCover({ gameId: game.id, system: current, cover })).game!));
            onClose();
          }} />
      );
    case 'merge':
      return (
        <GamePicker title={t('game.mergeTitle', { title: game.title })} excludeId={game.id} onClose={onClose}
          onPick={(target) => {
            if (!('id' in target)) return;
            const other = games.find((g) => g.id === target.id);
            if (!confirm(t('game.confirmMerge', { other: other?.title, title: game.title }))) return;
            run(async () => {
              const res = await gameClient.mergeGames({ targetId: game.id, sourceIds: [target.id] });
              dropGame(target.id);
              putGame(res.game!);
              onClose();
            });
          }} />
      );
    default:
      return null;
  }
}
