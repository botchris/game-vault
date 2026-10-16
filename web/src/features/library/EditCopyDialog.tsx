import { gameClient } from '../../api/client';
import { systemOf } from '../../lib/editions';
import type { Copy, CopyDetailsInput, Game } from '../../lib/model';
import { useAppData } from '../../state/AppData';
import CopyForm from './CopyForm';

/** The copy form for an existing copy: saves the change and closes. */
export default function EditCopyDialog({ game, copy, onClose }: { game: Game; copy: Copy; onClose: () => void }) {
  const { putGame, sourceName, systems } = useAppData();
  // The API exposes the effective system only: without an override, a system that differs from the
  // platform's is the source's.
  const d = copy.details!;
  const sourceSystem = !d.system && copy.effectiveSystem !== systemOf(d.platform, systems.map((s) => s.name)) ? copy.effectiveSystem : '';
  return (
    <CopyForm initial={{ ...d }} sourceSystem={sourceSystem} managedBy={copy.sourceId ? sourceName(copy.sourceId) : undefined} onClose={onClose}
      onSubmit={async (d: CopyDetailsInput) => {
        putGame((await gameClient.updateCopy({ gameId: game.id, copyId: copy.id, details: d })).game!);
        onClose();
      }} />
  );
}
