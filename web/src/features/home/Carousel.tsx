import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Icon } from '../../components/Icon';
import { railArrows } from '../../lib/home';

/** A row of cards that scrolls sideways with snap points, edge to edge: swiped on phones, with
 *  arrows on hover on desktops. An arrow shows only while there is more on its side, so a row whose
 *  cards fit has none, and the side with more cards fades out. The arrows scroll by most of a view;
 *  reduced motion jumps instead of gliding. */
export function Carousel({ label, children }: { label: string; children: ReactNode }) {
  const { t } = useTranslation();
  const track = useRef<HTMLUListElement>(null);
  const [arrows, setArrows] = useState({ prev: false, next: false });

  const update = useCallback(() => {
    const el = track.current;
    if (!el) return;
    const next = railArrows(el);
    setArrows((old) => (old.prev === next.prev && old.next === next.next ? old : next));
  }, []);

  // The cards change the row's width as they load (images, a refresh) and the window resizes it.
  useEffect(() => {
    const el = track.current;
    if (!el) return;
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    for (const child of el.children) observer.observe(child);
    return () => observer.disconnect();
  }, [update, children]);

  const scroll = (direction: 1 | -1) => {
    const el = track.current;
    if (!el) return;
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    el.scrollBy({ left: direction * el.clientWidth * 0.85, behavior: reduced ? 'auto' : 'smooth' });
  };

  const fades = `${arrows.prev ? ' more-before' : ''}${arrows.next ? ' more-after' : ''}`;
  return (
    <div className={`carousel${fades}`}>
      {arrows.prev && <button className="carousel-arrow prev" onClick={() => scroll(-1)} aria-label={t('home.previous')}><Icon name="prev" size={22} /></button>}
      <ul ref={track} className="carousel-track" aria-label={label} onScroll={update}>{children}</ul>
      {arrows.next && <button className="carousel-arrow next" onClick={() => scroll(1)} aria-label={t('home.next')}><Icon name="next" size={22} /></button>}
    </div>
  );
}

/** A titled row of the home page: its heading, what sits beside it (how old the data is, Refresh,
 *  See all), then the cards, or a line saying why there are none. */
export function HomeRow({ id, title, aside, none, children }: {
  id: string;
  title: string;
  aside?: ReactNode;
  /** Shown instead of the cards when there are none. */
  none?: string;
  children?: ReactNode;
}) {
  return (
    <section className="home-row" aria-labelledby={id}>
      <div className="home-row-head">
        <h2 id={id}>{title}</h2>
        {aside}
      </div>
      {none ? <p className="muted home-none">{none}</p> : <Carousel label={title}>{children}</Carousel>}
    </section>
  );
}
