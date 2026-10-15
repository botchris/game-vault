import { useTranslation } from 'react-i18next';
import { proxiedImage } from '../../api/client';
import type { HomeBlock, HomeDeal } from '../../gen/gamevault/v1/home_pb';
import { endsInDays, httpsLink } from '../../lib/home';
import { toDate } from '../../lib/model';
import { formatAmount } from '../../lib/money';
import { Carousel } from './Carousel';
import { ExternalBlock } from './ExternalBlock';

/** ITAD's terms ask for a credit and links through ITAD; prices and links are shown as given. */
const ITAD_URL = 'https://isthereanydeal.com/';

/** Deals of games the user does not own: ITAD's featured deals and its historical lows. */
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
      refreshing={props.refreshing} refreshError={props.refreshError} onRefresh={props.onRefresh}
      notConfigured={t('home.deals.notConfigured')}>
      <h3>{t('home.deals.featured')}</h3>
      <DealRow deals={props.featured} label={t('home.deals.featured')} />
      <h3>{t('home.deals.lows')}</h3>
      <DealRow deals={props.lows} label={t('home.deals.lows')} />
      <p className="muted small home-credit">
        {t('home.deals.credit')} <a href={ITAD_URL} target="_blank" rel="noopener noreferrer">IsThereAnyDeal</a>
      </p>
    </ExternalBlock>
  );
}

function DealRow({ deals, label }: { deals: HomeDeal[]; label: string }) {
  const { t } = useTranslation();
  if (!deals.length) return <p className="muted">{t('home.deals.none')}</p>;
  return <Carousel label={label}>{deals.map((d, i) => <DealCard key={`${d.title}-${d.shop}-${i}`} deal={d} />)}</Carousel>;
}

function DealCard({ deal: d }: { deal: HomeDeal }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const ends = endsInDays(toDate(d.expiresAt), new Date());
  const price = d.price ? formatAmount(d.price.amountMinor, d.price.currency, lang) : t('home.deals.free');
  const regular = d.regular && d.cut > 0 ? formatAmount(d.regular.amountMinor, d.regular.currency, lang) : '';
  // ITAD's link as it gave it (with its tags), and only over https.
  const href = httpsLink(d.url);

  const body = (
    <>
      <div className="deal-art">{d.imageUrl ? <img src={proxiedImage(d.imageUrl)} alt="" loading="lazy" /> : null}</div>
      <strong className="deal-title">{d.title}</strong>
      <span className="muted small">{d.shop}</span>
      <span className="deal-price">
        <b>{price}</b>
        {regular && <s className="muted">{regular}</s>}
        {d.cut > 0 && <span className="badge">{t('home.deals.cut', { cut: d.cut })}</span>}
      </span>
      {d.historicalLow && <span className="badge low">{t('home.deals.historicalLow')}</span>}
      {ends !== null && <span className="muted small">{ends === 0 ? t('home.deals.endsToday') : t('home.deals.endsIn', { count: ends })}</span>}
      {d.ownedOn.length > 0 && <span className="small">{t('home.deals.ownedOn', { systems: d.ownedOn.join(', ') })}</span>}
    </>
  );

  return (
    <li className="deal-card">
      {href
        ? <a className="deal-link" href={href} target="_blank" rel="noopener noreferrer">{body}</a>
        : <div className="deal-link">{body}</div>}
    </li>
  );
}
