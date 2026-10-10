import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { coverUrl } from '../api/client';
import type { Game } from '../gen/gamevault/v1/game_pb';
import { coverOrder, nextCover } from '../lib/editions';

/**
 * A game's cover on one edition (the main one when `system` is omitted). When the edition has no box
 * of its own (the server answers 404), it tries the main edition's, then the other editions', and
 * labels the borrowed box with its system; when none has one, it shows the title's initials and asks
 * no more. `onShown` reports the system whose box is shown, or null.
 */
export function Cover({ game, system = '', className = '', onShown }: {
  game: Game;
  system?: string;
  className?: string;
  onShown?: (system: string | null) => void;
}) {
  const { t } = useTranslation();
  const order = coverOrder(game, system);
  // Failures are forgotten when the game changes (a new cover was chosen) or the edition does.
  const version = `${game.id}|${game.updatedAt?.seconds ?? 0}|${order.join('|')}`;
  const [failed, setFailed] = useState<{ version: string; systems: ReadonlySet<string> }>({ version, systems: new Set() });
  const tried = failed.version === version ? failed.systems : new Set<string>();
  const shown = nextCover(order, tried);

  useEffect(() => { onShown?.(shown); }, [shown, onShown]);

  return (
    <div className={`cover ${className}`}>
      {shown === null ? (
        <div className="cover-placeholder" aria-hidden="true">
          <span>{initials(game.title)}</span>
        </div>
      ) : (
        <>
          <img key={shown} src={coverUrl(game, shown)} alt="" loading="lazy" decoding="async"
            onError={() => setFailed({ version, systems: new Set([...tried, shown]) })} />
          {shown !== order[0] && <span className="cover-borrowed">{t('edition.borrowed', { system: shown })}</span>}
        </>
      )}
    </div>
  );
}

function initials(title: string): string {
  return title
    .replace(/[^\p{L}\p{N} ]/gu, '')
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join('');
}
