import { useTranslation } from 'react-i18next';
import { Cover } from '../../components/Cover';
import { PlatformBadges } from '../../components/PlatformBadge';
import { daysUntil, isPendingKey, nextDeadline, type Game } from '../../lib/model';

/** Box-art view of the library: the covers are the interface. */
export default function PosterGrid({ games, onOpen, onPlatform, activePlatforms }: {
  games: Game[];
  onOpen: (id: string) => void;
  onPlatform?: (platform: string) => void;
  activePlatforms?: string[];
}) {
  const { t } = useTranslation();
  return (
    <ul className="posters">
      {games.map((g) => {
        const pending = g.copies.filter(isPendingKey).length;
        const redundant = g.copies.filter((c) => c.redundant).length;
        const days = daysUntil(nextDeadline(g));
        return (
          <li key={g.id} className="poster-card">
            {/* The title button stretches over the whole card; the platform badges sit above it. */}
            <span className="poster-art">
              <Cover game={g} />
              <span className="poster-flags">
                {days !== null && days <= 30 && <span className="flag danger">{t('common.daysLeft', { count: days })}</span>}
                {redundant > 0 && <span className="flag warn">{t('library.redundantBadge', { count: redundant })}</span>}
                {pending > 0 && redundant === 0 && <span className="flag">{t('library.pendingBadge', { count: pending })}</span>}
              </span>
              <span className="poster-platforms"><PlatformBadges game={g} onSelect={onPlatform} active={activePlatforms} /></span>
            </span>
            <button className="poster-open" onClick={() => onOpen(g.id)}>
              <span className="poster-title">{g.title}</span>
              {g.releaseYear > 0 && <span className="poster-meta">{g.releaseYear}</span>}
            </button>
          </li>
        );
      })}
    </ul>
  );
}
