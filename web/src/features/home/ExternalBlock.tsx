import { useEffect, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Icon } from '../../components/Icon';
import { Alert } from '../../components/ui';
import type { HomeBlock } from '../../gen/gamevault/v1/home_pb';
import { blockMode, canRefresh, refreshDelay, staleLine } from '../../lib/home';
import { toDate } from '../../lib/model';

/** What an external block shows: its rows, given what goes beside the first row's title (how old
 *  the data is when it is stale, and Refresh); or, under the block's name, a quiet skeleton of
 *  `shape` cards (while the page or a refresh loads), a "not configured" invitation that links to
 *  Providers, or a "not fetched yet" one with Refresh. */
export function ExternalBlock(props: {
  id: string;
  title: string;
  block: HomeBlock | undefined;
  /** The page's first read is still running. */
  loading: boolean;
  refreshing: boolean;
  refreshError: string;
  onRefresh: () => void;
  /** What the block brings, shown when no provider is enabled. */
  notConfigured: string;
  /** What the block says before its first gathering. */
  neverFetched: string;
  /** The shape of the block's cards, for the skeleton. */
  shape: 'deal' | 'poster';
  children: (aside: ReactNode) => ReactNode;
}) {
  const { t, i18n } = useTranslation();
  const { block } = props;
  const now = new Date();
  const mode = blockMode(block);
  const refreshable = !!block && mode !== 'notConfigured' && canRefresh(toDate(block.refreshableAt), now);
  const stale = block ? staleLine(block, toDate(block.fetchedAt), now, i18n.language, t) : null;

  // A disabled Refresh renders again when its time comes, so it does not stay disabled until
  // something else happens to re-render the page.
  const [, setTick] = useState(0);
  const refreshableMs = block ? toDate(block.refreshableAt)?.getTime() ?? 0 : 0;
  useEffect(() => {
    const delay = refreshDelay(refreshableMs ? new Date(refreshableMs) : null, new Date());
    if (delay === null) return;
    const timer = window.setTimeout(() => setTick((n) => n + 1), delay + 250);
    return () => window.clearTimeout(timer);
  }, [refreshableMs]);

  const refresh = block && mode !== 'notConfigured' && (
    <button className="home-refresh" onClick={props.onRefresh} disabled={!refreshable || props.refreshing}
      title={refreshable ? undefined : t('home.refreshSoon')}>
      <Icon name="refresh" size={15} /> {props.refreshing ? t('home.refreshing') : t('home.refresh')}
    </button>
  );
  const error = props.refreshError && <div className="home-pad"><Alert tone="error">{props.refreshError}</Alert></div>;

  // A refresh of data already on screen keeps it there; Refresh says it is working.
  if (!props.loading && mode === 'data') {
    return <>{error}{props.children(<>{stale && !props.refreshing && <span className="home-row-meta">{stale}</span>}{refresh}</>)}</>;
  }

  return (
    <section className="home-row" aria-labelledby={props.id} aria-busy={props.loading || props.refreshing}>
      <div className="home-row-head">
        <h2 id={props.id}>{props.title}</h2>
        {refresh}
      </div>
      {error}
      {props.loading || props.refreshing ? (
        <div className={`home-skeleton ${props.shape}`} aria-hidden="true"><span /><span /><span /><span /><span /><span /></div>
      ) : mode === 'notConfigured' ? (
        <div className="home-invite">
          <p>{props.notConfigured}</p>
          <a className="button" href="#/providers">{t('home.configure')}</a>
        </div>
      ) : (
        <div className="home-invite"><p className="muted">{props.neverFetched}</p></div>
      )}
    </section>
  );
}
