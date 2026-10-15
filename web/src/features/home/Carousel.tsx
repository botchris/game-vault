import { useRef, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Icon } from '../../components/Icon';

/** A row of cards that scrolls sideways with snap points: swiped on phones, with arrows on hover on
 *  desktops. The arrows scroll by most of a view; reduced motion jumps instead of gliding. */
export function Carousel({ label, children }: { label: string; children: ReactNode }) {
  const { t } = useTranslation();
  const track = useRef<HTMLUListElement>(null);

  const scroll = (direction: 1 | -1) => {
    const el = track.current;
    if (!el) return;
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    el.scrollBy({ left: direction * el.clientWidth * 0.9, behavior: reduced ? 'auto' : 'smooth' });
  };

  return (
    <div className="carousel">
      <button className="carousel-arrow prev" onClick={() => scroll(-1)} aria-label={t('home.previous')}><Icon name="prev" size={18} /></button>
      <ul ref={track} className="carousel-track" aria-label={label}>{children}</ul>
      <button className="carousel-arrow next" onClick={() => scroll(1)} aria-label={t('home.next')}><Icon name="next" size={18} /></button>
    </div>
  );
}
