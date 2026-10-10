import { useEffect, useReducer, useState, type CSSProperties, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { coverUrl } from '../api/client';
import type { Game } from '../gen/gamevault/v1/game_pb';
import { CoverFailures, coverOrder, coverSignature, nextCover } from '../lib/editions';
import { MAX_BOX_ASPECT, boxShape } from '../lib/shelf';

// Failed covers outlive the component: a card scrolled back into view, or the sheet's hero showing
// the same edition, does not ask again for a box the server did not have. A failure expires after a
// minute (it may have been a rate-limited answer), so the first render after that tries once more;
// nothing re-renders on a timer, so a card left alone sends nothing.
const failures = new CoverFailures();

// The natural aspect of every image already shown whole, by address: a card or hero that shows it
// again takes its shape at once, without waiting for the image to load.
const aspects = new Map<string, number>();

/**
 * A game's cover on one edition (the main one when `system` is omitted). When the edition has no box
 * of its own (the server answers 404), it tries the main edition's, then the other editions', and
 * labels the borrowed box with its system; when none has one, it shows the title's initials and asks
 * no more. `onShown` reports the system whose box is shown, or null.
 *
 * `whole` shows the box uncropped (see WholeCover, which `maxAspect` is passed to), with `children`
 * (badges, flags) laid over the visible box rather than the cell. Without it the image fills the cell, cropped: list thumbnails
 * and the blurred hero background.
 */
export function Cover({ game, system = '', className = '', onShown, whole = false, maxAspect, children }: {
  game: Game;
  system?: string;
  className?: string;
  onShown?: (system: string | null) => void;
  whole?: boolean;
  maxAspect?: number;
  children?: ReactNode;
}) {
  const { t } = useTranslation();
  const order = coverOrder(game, system);
  // Failures are forgotten when the game's cover inputs change (title, store links, copy platforms, a
  // chosen cover, an edition added or made main) or when they expire, not on every edit of the game.
  const signature = coverSignature(game);
  const [, rerender] = useReducer((n: number) => n + 1, 0);
  const shown = nextCover(order, failures.failed(game.id, signature));

  useEffect(() => { onShown?.(shown); }, [shown, onShown]);

  const placeholder = (
    <div className="cover-placeholder" aria-hidden="true">
      <span>{initials(game.title)}</span>
    </div>
  );
  const borrowed = shown !== null && shown !== order[0]
    ? <span className="cover-borrowed">{t('edition.borrowed', { system: shown })}</span>
    : null;

  if (whole) {
    return shown === null ? (
      <WholeCover className={className} placeholder={placeholder} maxAspect={maxAspect}>{children}</WholeCover>
    ) : (
      <WholeCover key={coverUrl(game, shown)} className={className} src={coverUrl(game, shown)} maxAspect={maxAspect}
        onError={() => { failures.add(game.id, signature, shown); rerender(); }}>
        {borrowed}{children}
      </WholeCover>
    );
  }

  return (
    <div className={`cover ${className}`}>
      {shown === null ? placeholder : (
        <>
          <img key={shown} src={coverUrl(game, shown)} alt="" loading="lazy" decoding="async"
            onError={() => { failures.add(game.id, signature, shown); rerender(); }} />
          {borrowed}
          {children}
        </>
      )}
    </div>
  );
}

/**
 * An image shown whole in a cover cell, with its own proportions, standing at the bottom of the cell
 * like a box on a shelf (a DS case short and wide, a Switch case tall and narrow). The cell keeps
 * its size, so titles and rows stay aligned; only the visible box (`.cover-box`) carries the rounded
 * corners and the shadow, and `children` are laid over it (`.cover-frame`), not over the empty part
 * of the cell. The box is invisible until the image has loaded and its shape is known, then appears
 * with no transition: the cell is reserved, so nothing moves, and the grid never animates. A `placeholder` instead of `src` fills the whole cell. Images wider than
 * `maxAspect` are cropped at the sides to it (see MAX_BOX_ASPECT).
 */
export function WholeCover({ src, className = '', onError, placeholder, maxAspect = MAX_BOX_ASPECT, children }: {
  src?: string;
  className?: string;
  onError?: () => void;
  placeholder?: ReactNode;
  maxAspect?: number;
  children?: ReactNode;
}) {
  // The measured aspect belongs to one address: a new src starts from what is known of it.
  const [measured, setMeasured] = useState<{ src: string; aspect: number } | null>(null);
  const aspect = src && measured?.src === src ? measured.aspect : src ? aspects.get(src) : undefined;
  const shown = !src || aspect !== undefined;
  const shape = aspect === undefined ? boxShape(0, 0) : boxShape(aspect, 1, maxAspect);
  const style = { '--box-w': `${shape.width}%`, '--box-h': `${shape.height}%` } as CSSProperties;

  return (
    <div className={`cover whole ${shown ? 'shown' : ''} ${className}`} style={style}>
      <div className="cover-box">
        {src ? (
          <img src={src} alt="" loading="lazy" decoding="async" onError={onError}
            onLoad={(e) => {
              const img = e.currentTarget;
              const a = img.naturalWidth > 0 && img.naturalHeight > 0 ? img.naturalWidth / img.naturalHeight : 2 / 3;
              aspects.set(src, a);
              setMeasured({ src, aspect: a });
            }} />
        ) : placeholder}
      </div>
      <div className="cover-frame">{children}</div>
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
