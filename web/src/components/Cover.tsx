import { useEffect, useReducer } from 'react';
import { useTranslation } from 'react-i18next';
import { coverUrl } from '../api/client';
import type { Game } from '../gen/gamevault/v1/game_pb';
import { CoverFailures, coverOrder, coverSignature, nextCover } from '../lib/editions';

// Failed covers outlive the component: a card scrolled back into view, or the sheet's hero showing
// the same edition, does not ask again for a box the server did not have.
const failures = new CoverFailures();

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
  // Failures are forgotten only when the game's cover inputs change (title, store links, a chosen
  // cover, an edition added or made main), not on every edit of the game.
  const signature = coverSignature(game);
  const [, rerender] = useReducer((n: number) => n + 1, 0);
  const shown = nextCover(order, failures.failed(game.id, signature));

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
            onError={() => { failures.add(game.id, signature, shown); rerender(); }} />
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
