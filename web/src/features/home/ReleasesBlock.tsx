import { useTranslation } from 'react-i18next';
import { proxiedImage } from '../../api/client';
import { useFormatters } from '../../components/ui';
import type { HomeBlock, HomeRelease } from '../../gen/gamevault/v1/home_pb';
import { gameHash } from '../../lib/gameRoute';
import { httpsLink } from '../../lib/home';
import { Carousel } from './Carousel';
import { ExternalBlock } from './ExternalBlock';

/** Upcoming games from IGDB: new entries in the user's series and the most anticipated ones. */
export function ReleasesBlock(props: {
  block: HomeBlock | undefined;
  fromSeries: HomeRelease[];
  anticipated: HomeRelease[];
  loading: boolean;
  refreshing: boolean;
  refreshError: string;
  onRefresh: () => void;
}) {
  const { t } = useTranslation();

  return (
    <ExternalBlock id="home-releases" title={t('home.releases.title')} block={props.block} loading={props.loading}
      refreshing={props.refreshing} refreshError={props.refreshError} onRefresh={props.onRefresh}
      notConfigured={t('home.releases.notConfigured')}>
      <h3>{t('home.releases.series')}</h3>
      <ReleaseRow releases={props.fromSeries} label={t('home.releases.series')} none={t('home.releases.noneSeries')} />
      <h3>{t('home.releases.anticipated')}</h3>
      <ReleaseRow releases={props.anticipated} label={t('home.releases.anticipated')} none={t('home.releases.noneAnticipated')} />
    </ExternalBlock>
  );
}

function ReleaseRow({ releases, label, none }: { releases: HomeRelease[]; label: string; none: string }) {
  if (!releases.length) return <p className="muted">{none}</p>;
  return <Carousel label={label}>{releases.map((r, i) => <ReleaseCard key={`${r.title}-${i}`} release={r} />)}</Carousel>;
}

function ReleaseCard({ release: r }: { release: HomeRelease }) {
  const { t } = useTranslation();
  const fmt = useFormatters();
  const when = r.releaseDate ? fmt.date(r.releaseDate) : t('home.releases.tba');
  const href = httpsLink(r.url);

  const body = (
    <>
      <div className="release-cover">{r.coverUrl ? <img src={proxiedImage(r.coverUrl)} alt="" loading="lazy" /> : null}</div>
      <strong className="deal-title">{r.title}</strong>
      <span className="muted small">{[when, r.platforms.join(', ')].filter(Boolean).join(' · ')}</span>
    </>
  );

  // Two links side by side, never nested: the card opens the database's page, the reason the
  // owned game's sheet.
  return (
    <li className="deal-card release-card">
      {href
        ? <a className="deal-link" href={href} target="_blank" rel="noopener noreferrer">{body}</a>
        : <div className="deal-link">{body}</div>}
      {r.becauseGameId && <a className="small release-because" href={gameHash(r.becauseGameId)}>{t('home.releases.because', { title: r.becauseTitle })}</a>}
    </li>
  );
}
