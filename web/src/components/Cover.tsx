import { useState } from 'react';
import { coverUrl } from '../api/client';
import type { Game } from '../gen/gamevault/v1/game_pb';

/** A game's cover (its main edition's, or the edition on `system`), or a placeholder with its
 *  initials when there is none. */
export function Cover({ game, system, className = '' }: { game: Game; system?: string; className?: string }) {
  const src = coverUrl(game, system);
  const [failedSrc, setFailedSrc] = useState('');
  const failed = failedSrc === src;
  return (
    <div className={`cover ${className}`}>
      {failed ? (
        <div className="cover-placeholder" aria-hidden="true">
          <span>{initials(game.title)}</span>
        </div>
      ) : (
        <img src={src} alt="" loading="lazy" decoding="async" onError={() => setFailedSrc(src)} />
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
