import { Code, ConnectError } from '@connectrpc/connect';
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, homeClient } from '../../api/client';
import { PageHeader } from '../../components/PageHeader';
import { Alert } from '../../components/ui';
import type { GetHomeResponse } from '../../gen/gamevault/v1/home_pb';
import { minutesUntil } from '../../lib/home';
import { latestOnly } from '../../lib/latest';
import { toDate } from '../../lib/model';
import { useAction, type Action } from '../../lib/useAction';
import { useAppData } from '../../state/AppData';
import { useHealth } from '../../state/Health';
import { CollectionBlock } from './CollectionBlock';
import { DealsBlock } from './DealsBlock';
import { homeEntrance, homeEntranceTaken } from './entrance';
import { PendingStrip } from './PendingStrip';
import { ReleasesBlock } from './ReleasesBlock';

/** The start page: what needs attention, current deals, upcoming releases and the collection at a
 *  glance. Deals and releases are read from what the server stored (gathered in the background);
 *  opening the page never asks an external service. Refresh asks one now. */
export default function HomePage() {
  const { t } = useTranslation();
  const { games } = useAppData();
  const { issues, reload: reloadHealth } = useHealth();
  // Decided on the first render and marked after it, so strict mode's second render keeps the
  // entrance; it fades only when the app opened on Home (see entrance.ts).
  const [entering] = useState(homeEntrance);
  useEffect(homeEntranceTaken, []);
  const [home, setHome] = useState<GetHomeResponse | null>(null);
  const [error, setError] = useState('');
  const deals = useAction();
  const releases = useAction();
  // Reads and refreshes overlap (a refresh, then the page read again after it fails): only the
  // newest answer may replace the home.
  const [latest] = useState(latestOnly);

  /** Reads the home; resolves to it, or null when the read failed or a newer one took over. */
  const load = useCallback(async () => {
    const current = latest();
    try {
      const res = await homeClient.getHome({});
      if (!current()) return null;
      setHome(res);
      setError('');
      return res;
    } catch (e) {
      if (current()) setError(errorMessage(e));
      return null;
    }
  }, [latest]);

  useEffect(() => {
    load();
  }, [load]);

  // A refresh answers with the whole home; a provider problem it met reaches System → Health too.
  // A failed one keeps the stored lists, so the page reads them again for the block's new state;
  // one asked too soon says when it may be asked again, from that read.
  const refresh = async (action: Action, block: 'deals' | 'releases') => {
    await action.run(async () => {
      const current = latest();
      try {
        const res = await homeClient.refreshHome({ block });
        if (res.home && current()) setHome(res.home);
      } catch (e) {
        const fresh = await load();
        if (e instanceof ConnectError && e.code === Code.ResourceExhausted) {
          const count = minutesUntil(toDate(fresh?.[block]?.refreshableAt), new Date());
          throw new Error(t('home.tooSoon', { count }));
        }
        throw e;
      }
    });
    reloadHealth();
  };

  const loading = home === null && !error;
  // When the home cannot be read, the page's alert says so; the blocks would otherwise claim to be
  // not configured.
  const unreadable = home === null && !!error;

  return (
    <div className={entering ? 'page home home-enter' : 'page home'}>
      <PageHeader title={t('home.title')} />
      <PendingStrip games={games} issues={issues} />
      {error && <Alert tone="error">{error}</Alert>}
      {!unreadable && <DealsBlock block={home?.deals} featured={home?.featured ?? []} lows={home?.historicalLows ?? []} loading={loading}
        refreshing={deals.busy} refreshError={deals.error} onRefresh={() => refresh(deals, 'deals')} />}
      {!unreadable && <ReleasesBlock block={home?.releases} fromSeries={home?.fromSeries ?? []} anticipated={home?.anticipated ?? []} loading={loading}
        refreshing={releases.busy} refreshError={releases.error} onRefresh={() => refresh(releases, 'releases')} />}
      <CollectionBlock games={games} />
    </div>
  );
}
