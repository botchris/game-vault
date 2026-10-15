import { Code, ConnectError } from '@connectrpc/connect';
import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, homeClient } from '../../api/client';
import { Alert } from '../../components/ui';
import type { GetHomeResponse } from '../../gen/gamevault/v1/home_pb';
import { blockMode, collectionSummary, heroSlides, minutesUntil } from '../../lib/home';
import { homeSync } from '../../lib/homeSync';
import { toDate } from '../../lib/model';
import { useAction, type Action } from '../../lib/useAction';
import { useAppData } from '../../state/AppData';
import { useHealth } from '../../state/Health';
import { CollectionRows, CollectionStats } from './CollectionBlock';
import { DealsBlock } from './DealsBlock';
import { homeEntrance, homeEntranceTaken } from './entrance';
import { HomeHero } from './HomeHero';
import { PendingStrip } from './PendingStrip';
import { ReleasesBlock } from './ReleasesBlock';

/** The start page, edge to edge: a hero (a slide of each kind: deals, releases, games), the collection in
 *  figures, what needs attention, then rows of cards: the deals, the upcoming releases, the newest
 *  games, the backlog and a tile per system. Deals and releases are read from what the server
 *  stored (gathered in the background); opening the page never asks an external service. Refresh
 *  asks one now. */
export default function HomePage() {
  const { t } = useTranslation();
  const { games } = useAppData();
  const summary = useMemo(() => collectionSummary(games, new Date()), [games]);
  const { issues, reload: reloadHealth } = useHealth();
  // Decided on the first render and marked after it, so strict mode's second render keeps the
  // entrance; it fades only when the app opened on Home (see entrance.ts).
  const [entering] = useState(homeEntrance);
  useEffect(homeEntranceTaken, []);
  const [home, setHome] = useState<GetHomeResponse | null>(null);
  const [error, setError] = useState('');
  const deals = useAction();
  const releases = useAction();
  // Reads and refreshes overlap (two blocks refreshed at once, a read after each): every refresh
  // reads the home again once it settles, and only the newest read is shown (see lib/homeSync.ts).
  const [sync] = useState(() => homeSync(() => homeClient.getHome({}), (res: GetHomeResponse) => {
    setHome(res);
    setError('');
  }, (e) => setError(errorMessage(e))));

  useEffect(() => {
    sync.load();
  }, [sync]);

  // A refresh's provider problem reaches System → Health too. A failed one keeps the stored lists,
  // and the read after it gives the block's new state; one asked too soon says when it may be asked
  // again, from that read.
  const refresh = async (action: Action, block: 'deals' | 'releases') => {
    await action.run(async () => {
      const { failed, error: e, fresh } = await sync.refresh(() => homeClient.refreshHome({ block }));
      if (!failed) return;
      if (e instanceof ConnectError && e.code === Code.ResourceExhausted) {
        const count = minutesUntil(toDate(fresh?.[block]?.refreshableAt), new Date());
        throw new Error(t('home.tooSoon', { count }));
      }
      throw e;
    });
    reloadHealth();
  };

  const loading = home === null && !error;
  // When the home cannot be read, the page's alert says so; the blocks would otherwise claim to be
  // not configured.
  const unreadable = home === null && !!error;
  const byId = useMemo(() => new Map(games.map((g) => [g.id, g])), [games]);
  const hero = useMemo(() => {
    const deals = blockMode(home?.deals) === 'data';
    const releases = blockMode(home?.releases) === 'data';
    return heroSlides({
      featured: deals ? home?.featured ?? [] : [],
      lows: deals ? home?.historicalLows ?? [] : [],
      series: releases ? home?.fromSeries ?? [] : [],
      anticipated: releases ? home?.anticipated ?? [] : [],
      recent: summary.recent,
      backlog: summary.backlogGames,
    }, new Date());
  }, [home, summary]);

  return (
    <div className={entering ? 'page home home-enter' : 'page home'}>
      <h1 className="sr-only">{t('home.title')}</h1>
      <HomeHero loading={loading} slides={hero} games={byId} dealsNotConfigured={!loading && blockMode(home?.deals) === 'notConfigured'} />
      <CollectionStats summary={summary} />
      <PendingStrip games={games} issues={issues} />
      {error && <div className="home-pad"><Alert tone="error">{error}</Alert></div>}
      {!unreadable && <DealsBlock block={home?.deals} featured={home?.featured ?? []} lows={home?.historicalLows ?? []} loading={loading}
        refreshing={deals.busy} refreshError={deals.error} onRefresh={() => refresh(deals, 'deals')} />}
      {!unreadable && <ReleasesBlock block={home?.releases} fromSeries={home?.fromSeries ?? []} anticipated={home?.anticipated ?? []} loading={loading}
        refreshing={releases.busy} refreshError={releases.error} onRefresh={() => refresh(releases, 'releases')} />}
      <CollectionRows games={games} summary={summary} />
    </div>
  );
}
