import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { valuationClient } from '../../api/client';
import { Cover } from '../../components/Cover';
import { platformLook } from '../../components/PlatformBadge';
import type { Game } from '../../gen/gamevault/v1/game_pb';
import { gameHash } from '../../lib/gameRoute';
import { pickValue, ROW, type CollectionSummary } from '../../lib/home';
import { formatAmount } from '../../lib/money';
import { HomeRow } from './Carousel';

/** The collection in four large figures under the hero, each opening its library view. The value of
 *  the physical copies is only shown when some copies are priced. */
export function CollectionStats({ summary }: { summary: CollectionSummary }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const [value, setValue] = useState<{ amountMinor: bigint; currency: string } | null>(null);

  // A failure just leaves the figure out.
  useEffect(() => {
    valuationClient.getCollectionValue({}).then((res) => setValue(pickValue(res.totals)), () => setValue(null));
  }, []);

  if (summary.games === 0) {
    return <p className="muted home-pad">{t('home.collection.empty')} <a href="#/sources">{t('home.collection.start')}</a></p>;
  }

  const n = (x: number) => x.toLocaleString(lang);
  return (
    <div className="home-stats home-pad" aria-label={t('home.collection.title')} role="group">
      <a className="home-stat" href="#/library">
        <strong>{n(summary.games)}</strong>
        <span>{t('home.collection.games', { count: summary.games })} · {t('home.collection.copies', { count: summary.copies, n: n(summary.copies) })}</span>
      </a>
      <a className="home-stat" href="#/library/recent">
        <strong>{n(summary.addedThisMonth)}</strong>
        <span>{t('home.collection.added')}</span>
      </a>
      <a className="home-stat" href="#/library/backlog">
        <strong>{n(summary.backlog)}</strong>
        <span>{t('home.collection.backlog')}</span>
      </a>
      {value && (
        <a className="home-stat" href="#/library/physical">
          <strong>{formatAmount(value.amountMinor, value.currency, lang)}</strong>
          <span>{t('home.collection.value')}</span>
        </a>
      )}
    </div>
  );
}

/** The collection's rows: the newest games, the backlog, and a tile per system. */
export function CollectionRows({ games, summary }: { games: Game[]; summary: CollectionSummary }) {
  const { t, i18n } = useTranslation();
  const byId = useMemo(() => new Map(games.map((g) => [g.id, g])), [games]);
  const pick = (ids: string[]) => ids.map((id) => byId.get(id)).filter((g): g is Game => !!g);
  if (summary.games === 0) return null;

  const n = (x: number) => x.toLocaleString(i18n.language);
  const total = summary.systems.reduce((sum, s) => sum + s.count, 0);
  const seeAll = (href: string) => <a className="home-row-more" href={href}>{t('home.collection.seeAll')}</a>;

  return (
    <>
      <HomeRow id="home-recent" title={t('home.collection.recent')} aside={seeAll('#/library/recent')}>
        {pick(summary.recent).map((g) => <GamePoster key={g.id} game={g} />)}
      </HomeRow>
      {summary.backlogGames.length > 0 && (
        <HomeRow id="home-backlog" title={t('home.collection.backlogRow')} aside={seeAll('#/library/backlog')}>
          {pick(summary.backlogGames.slice(0, ROW)).map((g) => <GamePoster key={g.id} game={g} />)}
        </HomeRow>
      )}
      <section className="home-row" aria-labelledby="home-systems">
        <div className="home-row-head"><h2 id="home-systems">{t('home.collection.systems')}</h2></div>
        <ul className="home-systems home-pad">
          {summary.systems.map((s) => {
            const name = s.rest ? t('home.collection.otherSystems') : s.system;
            return (
              <li key={s.system || 'rest'} className="home-system" style={s.rest ? undefined : { background: platformLook(s.system).bg }}>
                <strong>{n(s.count)}</strong>
                <span>{name}</span>
                <i style={{ width: `${Math.max(2, Math.round((s.count / total) * 100))}%` }} aria-hidden="true" />
              </li>
            );
          })}
        </ul>
      </section>
    </>
  );
}

function GamePoster({ game }: { game: Game }) {
  return (
    <li className="home-card is-poster">
      <a className="home-card-link" href={gameHash(game.id)} aria-label={game.title} title={game.title}>
        <Cover game={game} className="home-media is-poster" />
      </a>
    </li>
  );
}
