import { useTranslation } from 'react-i18next';
import { proxiedImage } from '../../api/client';
import type { HomeBlock, HomeDeal } from '../../gen/gamevault/v1/home_pb';
import { endsInDays, httpsLink, isBoxArt } from '../../lib/home';
import { toDate } from '../../lib/model';
import { formatAmount } from '../../lib/money';
import { HomeRow } from './Carousel';
import { ExternalBlock } from './ExternalBlock';

/** ITAD's terms ask for a credit and links through ITAD; prices and links are shown as given. */
const ITAD_URL = 'https://isthereanydeal.com/';

/** Deals of games the user does not own, as two rows: ITAD's featured deals and its historical lows. */
export function DealsBlock(props: {
  block: HomeBlock | undefined;
  featured: HomeDeal[];
  lows: HomeDeal[];
  loading: boolean;
  refreshing: boolean;
  refreshError: string;
  onRefresh: () => void;
}) {
  const { t } = useTranslation();

  return (
    <ExternalBlock id="home-deals" title={t('home.deals.title')} block={props.block} loading={props.loading}
      refreshing={props.refreshing} refreshError={props.refreshError} onRefresh={props.onRefresh} shape="deal"
      notConfigured={t('home.deals.notConfigured')}
      neverFetched={t('home.deals.neverFetched', { provider: props.block?.provider ?? '' })}>
      {(aside) => (
        <>
          <HomeRow id="home-featured" title={t('home.deals.featured')} aside={aside}
            none={props.featured.length ? undefined : t('home.deals.none')}>
            {props.featured.map((d, i) => <DealCard key={`${d.title}-${d.shop}-${i}`} deal={d} />)}
          </HomeRow>
          <HomeRow id="home-lows" title={t('home.deals.lows')} none={props.lows.length ? undefined : t('home.deals.none')}>
            {props.lows.map((d, i) => <DealCard key={`${d.title}-${d.shop}-${i}`} deal={d} />)}
          </HomeRow>
          <p className="muted small home-credit home-pad">
            {t('home.deals.credit')} <a href={ITAD_URL} target="_blank" rel="noopener noreferrer">IsThereAnyDeal</a>
          </p>
        </>
      )}
    </ExternalBlock>
  );
}

/** The price a deal costs now, "Free" when it has none. */
export function dealPrice(d: HomeDeal, lang: string, free: string): string {
  return d.price ? formatAmount(d.price.amountMinor, d.price.currency, lang) : free;
}

/** The regular price, struck through next to the price, only when there is a cut. */
export function dealRegular(d: HomeDeal, lang: string): string {
  return d.regular && d.cut > 0 ? formatAmount(d.regular.amountMinor, d.regular.currency, lang) : '';
}

function DealCard({ deal: d }: { deal: HomeDeal }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const ends = endsInDays(toDate(d.expiresAt), new Date());
  const owned = d.ownedOn.length ? new Intl.ListFormat(lang, { type: 'conjunction' }).format(d.ownedOn) : '';
  const regular = dealRegular(d, lang);
  // ITAD's link as it gave it (with its tags), and only over https.
  const href = httpsLink(d.url);

  const body = (
    <>
      <div className={`home-media is-wide${isBoxArt(d.imageUrl) ? ' tall-art' : ''}`}>
        {d.imageUrl ? <img src={proxiedImage(d.imageUrl)} alt="" loading="lazy" decoding="async" /> : <span className="home-media-title">{d.title}</span>}
        {d.cut > 0 && <span className="home-cut">{t('home.deals.cut', { cut: d.cut })}</span>}
        {d.historicalLow && <span className="home-low">{t('home.deals.historicalLow')}</span>}
      </div>
      <span className="home-card-line">
        <strong className="home-card-title">{d.title}</strong>
        <b className="home-price">{dealPrice(d, lang, t('home.deals.free'))}</b>
      </span>
      <span className="home-card-line muted small">
        <span>{d.shop}{ends !== null && ` · ${ends === 0 ? t('home.deals.endsToday') : t('home.deals.endsIn', { count: ends })}`}</span>
        {regular && <s>{regular}</s>}
      </span>
      {owned && <span className="small home-owned">{t('home.deals.ownedOn', { systems: owned })}</span>}
    </>
  );

  return (
    <li className="home-card is-deal">
      {href
        ? <a className="home-card-link" href={href} target="_blank" rel="noopener noreferrer">{body}</a>
        : <div className="home-card-link">{body}</div>}
    </li>
  );
}
