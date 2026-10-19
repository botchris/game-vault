import { useEffect, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { mediaUrl, metadataClient, proxiedImage } from '../../api/client';
import { Cover } from '../../components/Cover';
import { useFormatters } from '../../components/ui';
import type { Game } from '../../gen/gamevault/v1/game_pb';
import type { HomeDeal, HomeRelease } from '../../gen/gamevault/v1/home_pb';
import { gameHash } from '../../lib/gameRoute';
import { endsInDays, httpsLink, steamArt, type HeroSlide } from '../../lib/home';
import { toDate } from '../../lib/model';
import { dealPrice, dealRegular } from './DealsBlock';

/** A slide of the hero, as the page builds it (see heroSlides). */
export type Slide = HeroSlide<HomeDeal, HomeRelease>;

/** The top of the home page, edge to edge: a showcase of the page, one slide of each kind (a deal,
 *  a historical low, a release from the user's series, the newest game, an anticipated release, a
 *  backlog game; see heroSlides). Each slide shows its art over a blurred copy of itself that
 *  fills the band: a deal's banner (at most 600 pixels wide) at its size, a poster for the rest.
 *
 *  The slides take turns: a strip in the corner shows them all (on phones, as bars), the current
 *  one's bar fills in seven seconds and its end shows the next slide, so hovering or focusing the
 *  hero (which pauses the bar) holds the slide on screen, and reduced motion (no bar) never moves on
 *  by itself; the strip chooses a slide. */
export function HomeHero({ loading, slides, games, dealsNotConfigured }: {
  /** The page's first read is still running: the band waits empty rather than show the
   *  collection's slides and then add the deals and releases between them. */
  loading: boolean;
  slides: Slide[];
  /** The catalog, where the collection's slides find their games. */
  games: Map<string, Game>;
  /** No deals provider is enabled: the newest game's slide points to Providers. */
  dealsNotConfigured: boolean;
}) {
  const { t } = useTranslation();
  const [shown, setShown] = useState(0);
  const [held, setHeld] = useState(false);
  const shownSlides = slides.filter((s) => !('gameId' in s) || games.has(s.gameId));
  const index = shown < shownSlides.length ? shown : 0;

  if (loading) return <div className="home-hero" aria-hidden="true" />;
  if (!shownSlides.length) return null;

  const title = (s: Slide) => ('deal' in s ? s.deal.title : 'release' in s ? s.release.title : games.get(s.gameId)?.title ?? '');
  return (
    <section className={`home-hero${held ? ' held' : ''}`} aria-roledescription="carousel" aria-label={t('home.hero.label')}
      onMouseEnter={() => setHeld(true)} onMouseLeave={() => setHeld(false)}
      onFocus={() => setHeld(true)} onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) setHeld(false); }}>
      {shownSlides.map((s, i) => (
        <HeroSlideView key={`${s.kind}-${title(s)}`} slide={s} shown={i === index} games={games} dealsNotConfigured={dealsNotConfigured} />
      ))}
      {shownSlides.length > 1 && (
        <div className="home-hero-strip">
          {shownSlides.map((s, i) => (
            <button key={`${s.kind}-${title(s)}`} className={i === index ? 'on' : ''} aria-current={i === index}
              aria-label={t('home.hero.show', { title: title(s) })} onClick={() => setShown(i)}>
              <span className="home-hero-thumb">
                <Thumb slide={s} games={games} />
                {i === index && <i key={index} onAnimationEnd={() => setShown((index + 1) % shownSlides.length)} />}
              </span>
              <span className="home-hero-thumb-kind">{t(`home.hero.kind.${s.kind}`)}</span>
              <span className="home-hero-thumb-title">{title(s)}</span>
            </button>
          ))}
        </div>
      )}
    </section>
  );
}

function HeroSlideView({ slide, shown, games, dealsNotConfigured }: {
  slide: Slide;
  shown: boolean;
  games: Map<string, Game>;
  dealsNotConfigured: boolean;
}) {
  switch (slide.kind) {
    case 'deal':
    case 'low':
      return <DealSlide deal={slide.deal} low={slide.kind === 'low'} shown={shown} />;
    case 'series':
    case 'anticipated':
      return <ReleaseSlide release={slide.release} series={slide.kind === 'series'} shown={shown} />;
    default: {
      const game = games.get(slide.gameId);
      return game ? <GameSlide game={game} backlog={slide.kind === 'backlog'} shown={shown} dealsNotConfigured={dealsNotConfigured} /> : null;
    }
  }
}

/** A slide's picture in the strip of what comes next: its art, cropped to the thumbnail. */
function Thumb({ slide: s, games }: { slide: Slide; games: Map<string, Game> }) {
  if ('deal' in s) return <img src={proxiedImage(s.deal.imageUrl)} alt="" loading="lazy" decoding="async" />;
  if ('release' in s) return <img src={proxiedImage(s.release.artUrl || s.release.coverUrl)} alt="" loading="lazy" decoding="async" />;
  const game = games.get(s.gameId);
  return game ? <Cover game={game} /> : null;
}

/** The frame every slide shares: the blurred backdrop, the text, and the art beside it. A wide
 *  picture (a deal's banner) also fills the right of the band on desktops, fading into the text;
 *  phones show the art above the text instead. Slides that are not shown stay in the page (they
 *  cross-fade) but out of reach. */
function SlideFrame({ shown, backdrop, wide, art, children }: {
  shown: boolean;
  backdrop: ReactNode;
  /** The picture that fills the right of the band, for wide art. */
  wide?: string;
  art: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className={`home-hero-slide${shown ? ' on' : ''}${wide ? ' is-wide' : ''}`} aria-hidden={!shown} inert={!shown}>
      <div className="home-hero-backdrop">{backdrop}</div>
      {wide && <div className="home-hero-wide"><img src={wide} alt="" decoding="async" /></div>}
      <div className="home-hero-body">
        <div className="home-hero-text">{children}</div>
        {art}
      </div>
    </div>
  );
}

function DealSlide({ deal: d, low, shown }: { deal: HomeDeal; low: boolean; shown: boolean }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const art = proxiedImage(d.imageUrl);
  const regular = dealRegular(d, lang);
  const ends = endsInDays(toDate(d.expiresAt), new Date());
  // The featured deal's line says when it is also at its lowest; the low's own label says it already.
  const context = [t(low ? 'home.hero.kind.low' : 'home.hero.kind.deal'), !low && d.historicalLow ? t('home.deals.historicalLow') : '', d.shop];

  return (
    <SlideFrame shown={shown} backdrop={<img src={art} alt="" decoding="async" />} wide={art}
      art={<img className="home-hero-art" src={art} alt="" decoding="async" />}>
      <p className="home-hero-line">{context.filter(Boolean).join(' · ')}</p>
      <h2 className="home-hero-title">{d.title}</h2>
      <p className="home-hero-price">
        {d.cut > 0 && <span className="home-cut">{t('home.deals.cut', { cut: d.cut })}</span>}
        <b>{dealPrice(d, lang, t('home.deals.free'))}</b>
        {regular && <s>{regular}</s>}
      </p>
      {ends !== null && <p className="home-hero-line">{ends === 0 ? t('home.deals.endsToday') : t('home.deals.endsIn', { count: ends })}</p>}
      <div className="home-hero-actions">
        <a className="button primary" href={httpsLink(d.url)} target="_blank" rel="noopener noreferrer">{t('home.hero.see')}</a>
      </div>
    </SlideFrame>
  );
}

function ReleaseSlide({ release: r, series, shown }: { release: HomeRelease; series: boolean; shown: boolean }) {
  const { t, i18n } = useTranslation();
  const fmt = useFormatters();
  const cover = proxiedImage(r.coverUrl);
  // A release with wide art (IGDB's, else its Steam page's header) fills the band with it, like a deal;
  // without, its box takes that place.
  const art = r.artUrl ? proxiedImage(r.artUrl) : '';
  const when = r.releaseDate ? fmt.date(r.releaseDate) : t('home.releases.tba');
  const systems = new Intl.ListFormat(i18n.language, { type: 'unit' }).format(r.platforms);

  return (
    <SlideFrame shown={shown} backdrop={<img src={art || cover} alt="" decoding="async" />} wide={art || undefined}
      art={art ? <img className="home-hero-art" src={art} alt="" decoding="async" /> : <div className="home-hero-poster"><img src={cover} alt="" decoding="async" /></div>}>
      <p className="home-hero-line">{t(series ? 'home.hero.kind.series' : 'home.hero.kind.anticipated')}</p>
      <h2 className="home-hero-title">{r.title}</h2>
      <p className="home-hero-line">{[when, systems].filter(Boolean).join(' · ')}</p>
      <div className="home-hero-actions">
        <a className="button primary" href={httpsLink(r.url)} target="_blank" rel="noopener noreferrer">{t('home.hero.igdb')}</a>
        {series && r.becauseGameId && (
          <a className="button home-hero-ghost" href={gameHash(r.becauseGameId)}>{t('home.releases.because', { title: r.becauseTitle })}</a>
        )}
      </div>
    </SlideFrame>
  );
}

function GameSlide({ game, backlog, shown, dealsNotConfigured }: { game: Game; backlog: boolean; shown: boolean; dealsNotConfigured: boolean }) {
  const { t, i18n } = useTranslation();
  const systems = [...new Set(game.copies.map((c) => c.effectiveSystem).filter(Boolean))].join(' · ');
  // The sheet's first screenshot, else the header of the game's Steam page; without either, its box.
  const shot = useScreenshot(game.id, i18n.language);
  const steam = steamArt(game.links);
  const art = shot || (steam ? proxiedImage(steam) : '');

  return (
    <SlideFrame shown={shown} backdrop={art ? <img src={art} alt="" decoding="async" /> : <Cover game={game} />} wide={art || undefined}
      art={art
        ? <img className="home-hero-art" src={art} alt="" decoding="async" />
        : <a className="home-hero-poster" href={gameHash(game.id)} tabIndex={-1} aria-hidden="true"><Cover game={game} /></a>}>
      <p className="home-hero-line">{t(backlog ? 'home.hero.kind.backlog' : 'home.hero.kind.recent')}</p>
      <h2 className="home-hero-title">{game.title}</h2>
      {systems && <p className="home-hero-line">{systems}</p>}
      <div className="home-hero-actions">
        <a className="button primary" href={gameHash(game.id)}>{t('home.hero.open')}</a>
        {!backlog && dealsNotConfigured && <a className="button home-hero-ghost" href="#/providers">{t('home.hero.addDeals')}</a>}
      </div>
    </SlideFrame>
  );
}

/** The first screenshot of a game's sheet when the sheet was gathered before, "" otherwise: the hero
 *  shows it as the game's wide art. It reads the cached sheet only, so opening Home never makes the
 *  metadata providers send a request; a game without a sheet keeps its poster. */
function useScreenshot(id: string, language: string): string {
  const [shot, setShot] = useState('');

  useEffect(() => {
    let live = true;
    metadataClient.getGameDetails({ gameId: id, language, cachedOnly: true })
      .then((res) => { if (live) setShot(mediaUrl(res.details?.screenshots[0]?.fullUrl ?? '')); }, () => {});
    return () => { live = false; };
  }, [id, language]);

  return shot;
}
