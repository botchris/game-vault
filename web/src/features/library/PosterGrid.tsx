import { useTranslation } from 'react-i18next';
import { Cover } from '../../components/Cover';
import { PlatformBadge, PlatformBadges, SystemBadges } from '../../components/PlatformBadge';
import { itemLabel, type Grouping, type LibraryItem } from '../../lib/editions';
import { daysUntil, isPendingKey, nextDeadline, type Game } from '../../lib/model';

/** The badges of a library item: in "By platform", the edition's system (and, on PC, its stores,
 *  small); in "By game", every system of the game. */
export function ItemBadges({ item, grouping, onSystem, activeSystems = [], onOpen }: {
  item: LibraryItem<Game>;
  grouping: Grouping;
  onSystem?: (system: string) => void;
  activeSystems?: string[];
  /** Opens the item: a click on the stores' "+N" chip goes to the card like one on the stores. */
  onOpen?: () => void;
}) {
  if (grouping === 'game') return <SystemBadges game={item.game} onSelect={onSystem} active={activeSystems} />;
  if (!item.system) return null;
  return (
    <span className="pbadges">
      <PlatformBadge platform={item.system} onSelect={onSystem} active={activeSystems.includes(item.system)} />
      {item.system === 'PC' && <PlatformBadges game={item.view} small onMore={onOpen} />}
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
        const label = itemLabel(t, item, grouping);
        const year = item.game.releaseYear;
        // Item keys hold spaces and bars ("id|Xbox 360"); escaped, they make a valid id.
        const yearId = `poster-year-${encodeURIComponent(item.key)}`;
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
              <span className="poster-platforms"><ItemBadges item={item} grouping={grouping} onSystem={onSystem} activeSystems={activeSystems} onOpen={() => onOpen(item)} /></span>
            </span>
            {/* A label ("{title} · {system}") replaces the button's text, so the year is its description. */}
            <button className="poster-open" onClick={() => onOpen(item)} aria-label={label} aria-describedby={label && year ? yearId : undefined}>
              <span className="poster-title">{item.game.title}</span>
              {year > 0 && <span className="poster-meta" id={yearId}>{year}</span>}
            </button>
          </li>
        );
      })}
    </ul>
  );
}
