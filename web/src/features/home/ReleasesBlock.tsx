import { useTranslation } from 'react-i18next';
import { proxiedImage } from '../../api/client';
import { useFormatters } from '../../components/ui';
import type { HomeBlock, HomeRelease } from '../../gen/gamevault/v1/home_pb';
import { gameHash } from '../../lib/gameRoute';
import { httpsLink } from '../../lib/home';
import { HomeRow } from './Carousel';
import { ExternalBlock } from './ExternalBlock';

/** Upcoming games from IGDB, as two rows of posters: new entries in the user's series and the most
 *  anticipated ones. */
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
      refreshing={props.refreshing} refreshError={props.refreshError} onRefresh={props.onRefresh} shape="poster"
      notConfigured={t('home.releases.notConfigured')}
      neverFetched={t('home.releases.neverFetched', { provider: props.block?.provider ?? '' })}>
      {(aside) => (
        <>
          <HomeRow id="home-series" title={t('home.releases.series')} aside={aside}
            none={props.fromSeries.length ? undefined : t('home.releases.noneSeries')}>
            {props.fromSeries.map((r, i) => <ReleaseCard key={`${r.title}-${i}`} release={r} />)}
          </HomeRow>
          <HomeRow id="home-anticipated" title={t('home.releases.anticipated')}
            none={props.anticipated.length ? undefined : t('home.releases.noneAnticipated')}>
            {props.anticipated.map((r, i) => <ReleaseCard key={`${r.title}-${i}`} release={r} />)}
          </HomeRow>
        </>
      )}
    </ExternalBlock>
  );
}

function ReleaseCard({ release: r }: { release: HomeRelease }) {
  const { t, i18n } = useTranslation();
  const fmt = useFormatters();
  const systems = new Intl.ListFormat(i18n.language, { type: 'unit' }).format(r.platforms);
  const when = r.releaseDate ? fmt.date(r.releaseDate) : t('home.releases.tba');
  const href = httpsLink(r.url);

  // The date sits on the poster; the platforms take its place on hover, and are always written
  // under the title when there is no reason line (touch screens have no hover).
  const body = (
    <>
      <div className="home-media is-poster">
        {r.coverUrl ? <img src={proxiedImage(r.coverUrl)} alt="" loading="lazy" decoding="async" /> : <span className="home-media-title">{r.title}</span>}
        <span className="home-date">{when}</span>
        {systems && <span className="home-reveal">{systems}</span>}
      </div>
      <strong className="home-card-title">{r.title}</strong>
    </>
  );

  // Two links side by side, never nested: the card opens the database's page, the reason the
  // owned game's sheet.
  return (
    <li className="home-card is-poster">
      {href
        ? <a className="home-card-link" href={href} target="_blank" rel="noopener noreferrer">{body}</a>
        : <div className="home-card-link">{body}</div>}
      {r.becauseGameId
        ? <a className="small home-because" href={gameHash(r.becauseGameId)}>{t('home.releases.because', { title: r.becauseTitle })}</a>
        : systems && <span className="small muted home-because">{systems}</span>}
    </li>
  );
}
