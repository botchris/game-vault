import { useTranslation } from 'react-i18next';
import { MAX_RATING, PLAY_STATUSES, PlayStatus, playKey, type Game } from '../../lib/model';

/**
 * The game's play status and rating, in the sheet's hero. Both save on click, like a toggle: picking
 * the current status or star again clears it.
 */
export default function PlayControls({ game, busy, onChange }: {
  game: Game;
  busy: boolean;
  onChange: (patch: { playStatus?: PlayStatus; rating?: number }) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="play-controls">
      <div className="play-status" role="group" aria-label={t('play.label')}>
        {PLAY_STATUSES.map((s) => (
          <button key={s} type="button" className={`play-chip play-${playKey(s)} ${game.playStatus === s ? 'active' : ''}`}
            aria-pressed={game.playStatus === s} disabled={busy}
            onClick={() => onChange({ playStatus: game.playStatus === s ? PlayStatus.UNSPECIFIED : s })}>
            {t(`play.${playKey(s)}`)}
          </button>
        ))}
      </div>
      <Stars value={game.rating} disabled={busy} onChange={(rating) => onChange({ rating })} />
    </div>
  );
}

/** One to MAX_RATING stars; clicking the current rating clears it. Read-only without onChange. */
export function Stars({ value, disabled, onChange }: { value: number; disabled?: boolean; onChange?: (rating: number) => void }) {
  const { t } = useTranslation();
  const stars = Array.from({ length: MAX_RATING }, (_, i) => i + 1);
  if (!onChange) {
    return (
      <span className="stars read-only" role="img" aria-label={t('play.rated', { count: value })}>
        {stars.map((n) => <Star key={n} filled={n <= value} />)}
      </span>
    );
  }
  return (
    <div className="stars" role="group" aria-label={t('play.rating')}>
      {stars.map((n) => (
        <button key={n} type="button" className="star-button" disabled={disabled} aria-pressed={n === value}
          aria-label={t('play.rated', { count: n })} title={n === value ? t('play.clearRating') : t('play.rated', { count: n })}
          onClick={() => onChange(n === value ? 0 : n)}>
          <Star filled={n <= value} />
        </button>
      ))}
    </div>
  );
}

function Star({ filled }: { filled: boolean }) {
  return (
    <svg className={`star ${filled ? 'filled' : ''}`} width={18} height={18} viewBox="0 0 24 24" aria-hidden="true"
      fill={filled ? 'currentColor' : 'none'} stroke="currentColor" strokeWidth={1.8} strokeLinejoin="round">
      <path d="M12 3l2.7 5.6 6.1.9-4.4 4.3 1 6.1L12 17l-5.4 2.9 1-6.1-4.4-4.3 6.1-.9z" />
    </svg>
  );
}
