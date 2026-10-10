import { useTranslation } from 'react-i18next';
import { Cover } from '../../components/Cover';
import { PlatformBadge, PlatformBadges, SystemBadges } from '../../components/PlatformBadge';
import { itemLabel, type Grouping, type LibraryItem } from '../../lib/editions';
import { daysUntil, isPendingKey, nextDeadline, type Game } from '../../lib/model';

/** The badges of a library item: in "By platform", the edition's system (and, on PC, its stores,
 *  small); in "By game", every system of the game. */
export function ItemBadges({ item, grouping, onSystem, activeSystems = [] }: {
  item: LibraryItem<Game>;
  grouping: Grouping;
  onSystem?: (system: string) => void;
  activeSystems?: string[];
}) {
  if (grouping === 'game') return <SystemBadges game={item.game} onSelect={onSystem} active={activeSystems} />;
  if (!item.system) return null;
  return (
    <span className="pbadges">
      <PlatformBadge platform={item.system} onSelect={onSystem} active={activeSystems.includes(item.system)} />
      {item.system === 'PC' && <PlatformBadges game={item.view} small />}
    </span>
  );
}

/** Box-art view of the library: the covers are the interface. */
export default function PosterGrid({ items, grouping, onOpen, onSystem, activeSystems }: {
  items: LibraryItem<Game>[];
  grouping: Grouping;
  onOpen: (item: LibraryItem<Game>) => void;
  onSystem?: (system: string) => void;
  activeSystems?: string[];
}) {
  const { t } = useTranslation();
  return (
    <ul className="posters">
      {items.map((item) => {
        const v = item.view;
        const pending = v.copies.filter(isPendingKey).length;
        const redundant = v.copies.filter((c) => c.redundant).length;
        const days = daysUntil(nextDeadline(v));
        return (
          <li key={item.key} className="poster-card">
            {/* The title button stretches over the whole card; the badges sit above it. */}
            <span className="poster-art">
              <Cover game={item.game} system={item.system} />
              <span className="poster-flags">
                {days !== null && days <= 30 && <span className="flag danger">{t('common.daysLeft', { count: days })}</span>}
                {redundant > 0 && <span className="flag warn">{t('library.redundantBadge', { count: redundant })}</span>}
                {pending > 0 && redundant === 0 && <span className="flag">{t('library.pendingBadge', { count: pending })}</span>}
              </span>
              <span className="poster-platforms"><ItemBadges item={item} grouping={grouping} onSystem={onSystem} activeSystems={activeSystems} /></span>
            </span>
            <button className="poster-open" onClick={() => onOpen(item)} aria-label={itemLabel(t, item, grouping)}>
              <span className="poster-title">{item.game.title}</span>
              {item.game.releaseYear > 0 && <span className="poster-meta">{item.game.releaseYear}</span>}
            </button>
          </li>
        );
      })}
    </ul>
  );
}
