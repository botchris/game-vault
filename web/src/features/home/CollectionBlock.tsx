import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { valuationClient } from '../../api/client';
import { Cover } from '../../components/Cover';
import { platformLook } from '../../components/PlatformBadge';
import type { Game } from '../../gen/gamevault/v1/game_pb';
import { gameHash } from '../../lib/gameRoute';
import { collectionSummary, pickValue } from '../../lib/home';
import { formatAmount } from '../../lib/money';

/** The collection at a glance: four figures that open their library views, the newest covers, and
 *  the share of each system. */
export function CollectionBlock({ games }: { games: Game[] }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const summary = useMemo(() => collectionSummary(games, new Date()), [games]);
  const byId = useMemo(() => new Map(games.map((g) => [g.id, g])), [games]);
  const [value, setValue] = useState<{ amountMinor: bigint; currency: string } | null>(null);

  // The value is only shown when some copies are priced; a failure just leaves the figure out.
  useEffect(() => {
    valuationClient.getCollectionValue({}).then((res) => setValue(pickValue(res.totals)), () => setValue(null));
  }, []);

  const n = (x: number) => x.toLocaleString(lang);
  const systemName = (s: { system: string; rest: boolean }) => (s.rest ? t('home.collection.otherSystems') : s.system);
  const shareLabel = new Intl.ListFormat(lang, { type: 'unit' })
    .format(summary.systems.map((s) => t('home.collection.systemCount', { system: systemName(s), n: n(s.count) })));

  return (
    <section className="card home-block" aria-labelledby="home-collection">
      <h2 id="home-collection">{t('home.collection.title')}</h2>
      {summary.games === 0 ? (
        <p className="muted">{t('home.collection.empty')} <a href="#/sources">{t('home.collection.start')}</a></p>
      ) : (
        <>
          <div className="home-figures">
            <a className="home-figure" href="#/library">
              <strong>{n(summary.games)}</strong>
              <span>{t('home.collection.games', { count: summary.games })}</span>
              <span className="muted small">{t('home.collection.copies', { count: summary.copies, n: n(summary.copies) })}</span>
            </a>
            <a className="home-figure" href="#/library/recent">
              <strong>{n(summary.addedThisMonth)}</strong>
              <span>{t('home.collection.added')}</span>
            </a>
            <a className="home-figure" href="#/library/backlog">
              <strong>{n(summary.backlog)}</strong>
              <span>{t('home.collection.backlog')}</span>
            </a>
            {value && (
              <a className="home-figure" href="#/library/physical">
                <strong>{formatAmount(value.amountMinor, value.currency, lang)}</strong>
                <span>{t('home.collection.value')}</span>
              </a>
            )}
          </div>

          <h3>{t('home.collection.recent')}</h3>
          <ul className="home-recent">
            {summary.recent.map((id) => byId.get(id)).filter((g): g is Game => !!g).map((g) => (
              <li key={g.id}><a href={gameHash(g.id)} title={g.title} aria-label={g.title}><Cover game={g} /></a></li>
            ))}
          </ul>

          <h3>{t('home.collection.systems')}</h3>
          <div className="home-share" role="img"
            aria-label={shareLabel}>
            {summary.systems.map((s) => (
              <span key={s.system || 'rest'} style={{ flexGrow: s.count, background: s.rest ? 'var(--surface-3)' : platformLook(s.system).bg }} />
            ))}
          </div>
          <ul className="home-share-legend">
            {summary.systems.map((s) => (
              <li key={s.system || 'rest'}>
                <i style={{ background: s.rest ? 'var(--surface-3)' : platformLook(s.system).bg }} aria-hidden="true" />
                {systemName(s)} <span className="muted">{n(s.count)}</span>
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}
