import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, homeClient } from '../../api/client';
import { PageHeader } from '../../components/PageHeader';
import { Alert } from '../../components/ui';
import type { GetHomeResponse } from '../../gen/gamevault/v1/home_pb';
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

  const load = useCallback(async () => {
    try {
      setHome(await homeClient.getHome({}));
      setError('');
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // A refresh answers with the whole home; a provider problem it met reaches System → Health too.
  // A failed one keeps the stored lists, so the page reads them again for the block's new state.
  const refresh = async (action: Action, block: 'deals' | 'releases') => {
    const ok = await action.run(async () => {
      const res = await homeClient.refreshHome({ block });
      if (res.home) setHome(res.home);
    });
    if (!ok) await load();
    reloadHealth();
  };

  const loading = home === null && !error;

  return (
    <div className={entering ? 'page home home-enter' : 'page home'}>
      <PageHeader title={t('home.title')} />
      <PendingStrip games={games} issues={issues} />
      {error && <Alert tone="error">{error}</Alert>}
      <DealsBlock block={home?.deals} featured={home?.featured ?? []} lows={home?.historicalLows ?? []} loading={loading}
        refreshing={deals.busy} refreshError={deals.error} onRefresh={() => refresh(deals, 'deals')} />
      <ReleasesBlock block={home?.releases} fromSeries={home?.fromSeries ?? []} anticipated={home?.anticipated ?? []} loading={loading}
        refreshing={releases.busy} refreshError={releases.error} onRefresh={() => refresh(releases, 'releases')} />
      <CollectionBlock games={games} />
    </div>
  );
}
