import { useEffect, useReducer, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { coverUrl } from '../api/client';
import type { Game } from '../gen/gamevault/v1/game_pb';
import { CoverFailures, coverOrder, coverSignature, nextCover } from '../lib/editions';
import { fillsCell } from '../lib/coverFit';

// Failed covers outlive the component: a card scrolled back into view, or the sheet's hero showing
// the same edition, does not ask again for a box the server did not have. A failure expires after a
// minute (it may have been a rate-limited answer), so the first render after that tries once more;
// nothing re-renders on a timer, so a card left alone sends nothing.
const failures = new CoverFailures();

// Whether each image already shown whole fills its cell, by address: a card or hero that shows it
// again mounts its backdrop at once, without waiting for the image to load.
const fits = new Map<string, boolean>();

/**
 * A game's cover on one edition (the main one when `system` is omitted). When the edition has no box
 * of its own (the server answers 404), it tries the main edition's, then the other editions', and
 * labels the borrowed box with its system; when none has one, it shows the title's initials and asks
 * no more. `onShown` reports the system whose box is shown, or null.
 *
 * `whole` shows the box uncropped, on a blurred copy of itself when it leaves gaps (see WholeCover), with `children`
 * (badges, flags) laid over the cell. Without it the image fills the cell, cropped: list thumbnails
 * and the blurred hero background.
 */
export function Cover({ game, system = '', className = '', onShown, whole = false, children }: {
  game: Game;
  system?: string;
  className?: string;
  onShown?: (system: string | null) => void;
  whole?: boolean;
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
      <WholeCover className={className} placeholder={placeholder}>{children}</WholeCover>
    ) : (
      <WholeCover key={coverUrl(game, shown)} className={className} src={coverUrl(game, shown)}
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
 * An image shown whole in a cover cell. Every cell keeps the same 2:3 size, so titles and rows stay
 * aligned; the image is contained and centred in it. When it does not fill the cell (a DS case, a
 * Switch case, a banner; see fillsCell), the same image, cropped to cover, blurred and darkened, fills
 * the gaps behind it. That backdrop is mounted only once the image has loaded and its shape is known
 * (or remembered from an earlier load), with no transition: the grid never animates. The corners and
 * shadow sit on `.cover-box`, which fills the cell; `children` (badges, flags) are laid over it in
 * `.cover-frame`. Hover lifts those two, never the cell (see ui.md). A `placeholder` instead of `src`
 * fills the cell. `noReferrer` loads an image from another site without telling it where it is shown
 * (the cover picker's preview of a pasted address).
 */
export function WholeCover({ src, className = '', onError, placeholder, noReferrer = false, children }: {
  src?: string;
  className?: string;
  onError?: () => void;
  noReferrer?: boolean;
  placeholder?: ReactNode;
  children?: ReactNode;
}) {
  // What was measured belongs to one address: a new src starts from what is remembered of it.
  const [measured, setMeasured] = useState<{ src: string; fills: boolean } | null>(null);
  const fills = !src ? true : measured?.src === src ? measured.fills : fits.get(src);
  const referrer = noReferrer ? 'no-referrer' : undefined;

  return (
    <div className={`cover whole ${className}`}>
      <div className="cover-box">
        {src && fills === false && <img className="cover-backdrop" src={src} alt="" aria-hidden="true" decoding="async" referrerPolicy={referrer} />}
        {src ? (
          <img className="cover-image" src={src} alt="" loading="lazy" decoding="async" onError={onError} referrerPolicy={referrer}
            onLoad={(e) => {
              const f = fillsCell(e.currentTarget.naturalWidth, e.currentTarget.naturalHeight);
              fits.set(src, f);
              setMeasured({ src, fills: f });
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
