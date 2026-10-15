import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { proxiedImage } from '../../api/client';
import { Cover } from '../../components/Cover';
import type { Game } from '../../gen/gamevault/v1/game_pb';
import type { HomeDeal } from '../../gen/gamevault/v1/home_pb';
import { gameHash } from '../../lib/gameRoute';
import { endsInDays, httpsLink } from '../../lib/home';
import { toDate } from '../../lib/model';
import { dealPrice, dealRegular } from './DealsBlock';

/** The top of the home page, edge to edge: the featured deals one after another (see heroDeals),
 *  or, without deals, the newest game in the collection. A deal's art is at most 600 pixels wide,
 *  so it is shown at its size beside the text, over a blurred copy of itself that fills the band.
 *
 *  The deals take turns: each dot's bar fills in seven seconds and its end shows the next deal, so
 *  hovering or focusing the hero (which pauses the bar) holds the deal on screen, and reduced motion
 *  (no bar) never moves on by itself; the dots choose a deal. */
export function HomeHero({ loading, deals, newest, dealsNotConfigured }: {
  /** The page's first read is still running: the band waits empty rather than show the newest
   *  game and then swap it for the deals. */
  loading: boolean;
  deals: HomeDeal[];
  newest: Game | undefined;
  /** No deals provider is enabled: the newest game's hero points to Providers. */
  dealsNotConfigured: boolean;
}) {
  const { t } = useTranslation();
  const [shown, setShown] = useState(0);
  const [held, setHeld] = useState(false);
  const index = shown < deals.length ? shown : 0;

  if (loading) return <div className="home-hero" aria-hidden="true" />;
  if (!deals.length) return newest ? <GameHero game={newest} dealsNotConfigured={dealsNotConfigured} /> : null;

  return (
    <section className={`home-hero${held ? ' held' : ''}`} aria-roledescription="carousel" aria-label={t('home.hero.label')}
      onMouseEnter={() => setHeld(true)} onMouseLeave={() => setHeld(false)}
      onFocus={() => setHeld(true)} onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) setHeld(false); }}>
      {deals.map((d, i) => <DealSlide key={`${d.title}-${d.shop}`} deal={d} shown={i === index} />)}
      {deals.length > 1 && (
        <div className="home-hero-dots">
          {deals.map((d, i) => (
            <button key={`${d.title}-${d.shop}`} className={i === index ? 'on' : ''} aria-current={i === index}
              aria-label={t('home.hero.show', { title: d.title })} onClick={() => setShown(i)}>
              {i === index && <i key={index} onAnimationEnd={() => setShown((index + 1) % deals.length)} />}
            </button>
          ))}
        </div>
      )}
    </section>
  );
}

function DealSlide({ deal: d, shown }: { deal: HomeDeal; shown: boolean }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const art = proxiedImage(d.imageUrl);
  const regular = dealRegular(d, lang);
  const ends = endsInDays(toDate(d.expiresAt), new Date());

  // Slides that are not shown stay in the page (they cross-fade) but out of reach.
  return (
    <div className={`home-hero-slide${shown ? ' on' : ''}`} aria-hidden={!shown} inert={!shown}>
      <div className="home-hero-backdrop"><img src={art} alt="" decoding="async" /></div>
      <div className="home-hero-body">
        <div className="home-hero-text">
          <h2 className="home-hero-title">{d.title}</h2>
          <p className="home-hero-price">
            {d.cut > 0 && <span className="home-cut">{t('home.deals.cut', { cut: d.cut })}</span>}
            <b>{dealPrice(d, lang, t('home.deals.free'))}</b>
            {regular && <s>{regular}</s>}
          </p>
          <p className="home-hero-line">
            {[d.historicalLow ? t('home.deals.historicalLow') : '', d.shop,
              ends === null ? '' : ends === 0 ? t('home.deals.endsToday') : t('home.deals.endsIn', { count: ends })].filter(Boolean).join(' · ')}
          </p>
          <a className="button primary" href={httpsLink(d.url)} target="_blank" rel="noopener noreferrer">{t('home.hero.see')}</a>
        </div>
        <img className="home-hero-art" src={art} alt="" decoding="async" />
      </div>
    </div>
  );
}

function GameHero({ game, dealsNotConfigured }: { game: Game; dealsNotConfigured: boolean }) {
  const { t } = useTranslation();
  const systems = [...new Set(game.copies.map((c) => c.effectiveSystem).filter(Boolean))].join(' · ');

  return (
    <section className="home-hero" aria-label={t('home.hero.newest')}>
      <div className="home-hero-slide on">
        <div className="home-hero-backdrop"><Cover game={game} /></div>
        <div className="home-hero-body">
          <div className="home-hero-text">
            <p className="home-hero-line">{t('home.hero.newest')}</p>
            <h2 className="home-hero-title">{game.title}</h2>
            {systems && <p className="home-hero-line">{systems}</p>}
            <div className="home-hero-actions">
              <a className="button primary" href={gameHash(game.id)}>{t('home.hero.open')}</a>
              {dealsNotConfigured && <a className="button home-hero-ghost" href="#/providers">{t('home.hero.addDeals')}</a>}
            </div>
          </div>
          <a className="home-hero-poster" href={gameHash(game.id)} tabIndex={-1} aria-hidden="true"><Cover game={game} /></a>
        </div>
      </div>
    </section>
  );
}
