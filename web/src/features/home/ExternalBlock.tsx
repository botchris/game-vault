import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Icon } from '../../components/Icon';
import { Alert } from '../../components/ui';
import type { HomeBlock } from '../../gen/gamevault/v1/home_pb';
import { blockMode, canRefresh, staleLine } from '../../lib/home';
import { toDate } from '../../lib/model';

/** The frame of an external block: its title and Refresh button, then either a quiet skeleton
 *  (while the page or a refresh loads), a "not configured" card that links to Providers, a "not
 *  fetched yet" card, or the data with a line saying how old it is when it is stale. */
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
  children: ReactNode;
}) {
  const { t, i18n } = useTranslation();
  const { block } = props;
  const now = new Date();
  const mode = blockMode(block);
  const refreshable = !!block && mode !== 'notConfigured' && canRefresh(toDate(block.refreshableAt), now);
  const stale = block ? staleLine(block, toDate(block.fetchedAt), now, i18n.language, t) : null;

  return (
    <section className="card home-block" aria-labelledby={props.id} aria-busy={props.loading || props.refreshing}>
      <div className="home-block-head">
        <h2 id={props.id}>{props.title}</h2>
        {block && mode !== 'notConfigured' && (
          <button className="home-refresh" onClick={props.onRefresh} disabled={!refreshable || props.refreshing}
            title={refreshable ? undefined : t('home.refreshSoon')}>
            <Icon name="refresh" size={16} /> {props.refreshing ? t('home.refreshing') : t('home.refresh')}
          </button>
        )}
      </div>
      {props.refreshError && <Alert tone="error">{props.refreshError}</Alert>}
      {props.loading || (props.refreshing && mode === 'neverFetched') ? (
        <div className="home-skeleton" aria-hidden="true"><span /><span /><span /><span /></div>
      ) : mode === 'notConfigured' ? (
        <div className="home-empty">
          <p>{props.notConfigured}</p>
          <a className="button" href="#/providers">{t('home.configure')}</a>
        </div>
      ) : mode === 'neverFetched' ? (
        <div className="home-empty"><p className="muted">{t('home.neverFetched', { provider: block?.provider ?? '' })}</p></div>
      ) : (
        <>
          {stale && <p className="muted small home-stale">{stale}</p>}
          {props.children}
        </>
      )}
    </section>
  );
}
