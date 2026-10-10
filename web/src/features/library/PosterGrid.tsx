import { useTranslation } from 'react-i18next';
import { Cover } from '../../components/Cover';
import { PlatformBadge, PlatformBadges, SystemBadges, hiddenPlatforms } from '../../components/PlatformBadge';
import { itemLabel, type Grouping, type LibraryItem } from '../../lib/editions';
import { daysUntil, isPendingKey, nextDeadline, type Game } from '../../lib/model';

/** The badges of a library item: in "By platform", the edition's system (and, on PC, its stores,
 *  small); in "By game", every system of the game. */
export function ItemBadges({ item, grouping, onSystem, activeSystems = [], onOpen, moreId }: {
  item: LibraryItem<Game>;
  grouping: Grouping;
  onSystem?: (system: string) => void;
  activeSystems?: string[];
  /** Opens the item: a click on the stores' "+N" chip goes to the card like one on the stores. */
  onOpen?: () => void;
  /** Id for the stores' "+N" chip, so the item's button can be described by the stores it hides. */
  moreId?: string;
}) {
  if (grouping === 'game') return <SystemBadges game={item.game} onSelect={onSystem} active={activeSystems} />;
  if (!item.system) return null;
  return (
    <span className="pbadges">
      <PlatformBadge platform={item.system} onSelect={onSystem} active={activeSystems.includes(item.system)} />
      {item.system === 'PC' && <PlatformBadges game={item.view} small onMore={onOpen} moreId={moreId} />}
    </span>
  );
}

/** The id of an item's "+N" stores chip when it has one (a PC edition in "By platform" with more
 *  stores than fit), to describe the item's open button with the stores it hides. */
export function moreStoresId(prefix: string, item: LibraryItem<Game>, grouping: Grouping): string | undefined {
  if (grouping !== 'platform' || item.system !== 'PC' || hiddenPlatforms(item.view).length === 0) return undefined;
  return `${prefix}-more-${encodeURIComponent(item.key)}`;
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
        const moreId = moreStoresId('poster', item, grouping);
        // A label ("{title} · {system}") replaces the button's text, so the year joins its description.
        const describedBy = [label && year ? yearId : '', moreId ?? ''].filter(Boolean).join(' ') || undefined;
        return (
          <li key={item.key} className="poster-card">
            {/* The title button stretches over the whole card; the badges sit above it. */}
            <span className="poster-art">
              {/* The box is shown whole; flags and badges sit on the box, not on the cell around it. */}
              <Cover game={item.game} system={item.system} whole>
                <span className="poster-flags">
                  {days !== null && days <= 30 && <span className="flag danger">{t('common.daysLeft', { count: days })}</span>}
                  {redundant > 0 && <span className="flag warn">{t('library.redundantBadge', { count: redundant })}</span>}
                  {pending > 0 && redundant === 0 && <span className="flag">{t('library.pendingBadge', { count: pending })}</span>}
                </span>
                <span className="poster-platforms"><ItemBadges item={item} grouping={grouping} onSystem={onSystem} activeSystems={activeSystems} onOpen={() => onOpen(item)} moreId={moreId} /></span>
              </Cover>
            </span>
            <button className="poster-open" onClick={() => onOpen(item)} aria-label={label} aria-describedby={describedBy}>
              <span className="poster-title">{item.game.title}</span>
              {year > 0 && <span className="poster-meta" id={yearId}>{year}</span>}
            </button>
          </li>
        );
      })}
    </ul>
  );
}
