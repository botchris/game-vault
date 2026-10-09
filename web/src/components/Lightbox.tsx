import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Icon } from './Icon';

const MAX_SCALE = 5;
const STEP = 1.5;
const DOUBLE_TAP_MS = 300;
const SWIPE_PX = 50;

interface View { scale: number; x: number; y: number }
const FIT: View = { scale: 1, x: 0, y: 0 };

/**
 * Full-screen image viewer for every gallery (screenshots, copy photos): previous / next, zoom
 * (buttons, double click or tap, Ctrl/⌘ + wheel, trackpad or touch pinch, drag to pan), full
 * screen, swipe on touch screens and the keyboard (← → Esc + − 0 F). `footer` shows per-image
 * content under the image.
 */
export default function Lightbox({ images, index, onIndex, onClose, label, footer }: {
  images: string[];
  index: number;
  onIndex: (i: number) => void;
  onClose: () => void;
  label?: string;
  footer?: ReactNode;
}) {
  const { t } = useTranslation();
  const root = useRef<HTMLDivElement>(null);
  const stage = useRef<HTMLDivElement>(null);
  const img = useRef<HTMLImageElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const [view, setView] = useState<View>(FIT);
  const [fullscreen, setFullscreen] = useState(false);
  const [closing, setClosing] = useState(false);
  const many = images.length > 1;
  const canFullscreen = typeof document !== 'undefined' && document.fullscreenEnabled;

  const go = useCallback((delta: number) => {
    if (many) onIndex((index + delta + images.length) % images.length);
  }, [many, index, images.length, onIndex]);

  const close = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen();
    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reduce) { onClose(); return; }
    setClosing(true);
    window.setTimeout(onClose, 160);
  }, [onClose]);

  // Keeps the image inside the stage: it can only be dragged as far as its zoomed edges.
  const clamp = useCallback((v: View): View => {
    const s = stage.current, i = img.current;
    if (!s || !i) return v;
    const scale = Math.min(MAX_SCALE, Math.max(1, v.scale));
    const maxX = Math.max(0, (i.offsetWidth * scale - s.clientWidth) / 2);
    const maxY = Math.max(0, (i.offsetHeight * scale - s.clientHeight) / 2);
    return { scale, x: Math.min(maxX, Math.max(-maxX, v.x)), y: Math.min(maxY, Math.max(-maxY, v.y)) };
  }, []);

  /** Zooms to scale keeping the point (px, py), relative to the stage's centre, still. */
  const zoomAt = useCallback((scale: number, px = 0, py = 0) => {
    setView((v) => {
      const next = Math.min(MAX_SCALE, Math.max(1, scale));
      const k = next / v.scale;
      return clamp({ scale: next, x: px - (px - v.x) * k, y: py - (py - v.y) * k });
    });
  }, [clamp]);

  const toggleFullscreen = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen();
    else void root.current?.requestFullscreen();
  }, []);

  // A new image starts fitted.
  useEffect(() => setView(FIT), [index]);

  // Neighbouring images are loaded ahead, so moving through the gallery feels instant.
  useEffect(() => {
    if (!many) return;
    for (const d of [-1, 1]) new Image().src = images[(index + d + images.length) % images.length]!;
  }, [index, images, many]);

  // Focus goes into the viewer and back where it was on close.
  useEffect(() => {
    const before = document.activeElement as HTMLElement | null;
    closeButton.current?.focus();
    return () => before?.focus();
  }, []);

  useEffect(() => {
    const onChange = () => setFullscreen(!!document.fullscreenElement);
    document.addEventListener('fullscreenchange', onChange);
    return () => document.removeEventListener('fullscreenchange', onChange);
  }, []);

  // Capture phase + stopPropagation: these keys act on the viewer, not on the dialog behind it.
  // Keys typed into a field (the caption) are left alone.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      if (target?.closest('input, textarea, [contenteditable="true"]')) return;
      const actions: Record<string, () => void> = {
        Escape: close,
        ArrowLeft: () => go(-1),
        ArrowRight: () => go(1),
        '+': () => zoomAt(view.scale * STEP),
        '=': () => zoomAt(view.scale * STEP),
        '-': () => zoomAt(view.scale / STEP),
        '0': () => setView(FIT),
        f: () => canFullscreen && toggleFullscreen(),
      };
      const action = actions[e.key];
      if (!action) return;
      e.stopPropagation();
      e.preventDefault();
      action();
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [close, go, zoomAt, view.scale, canFullscreen, toggleFullscreen]);

  // Ctrl/⌘ + wheel and trackpad pinches (which arrive as Ctrl + wheel) zoom at the pointer; a plain
  // wheel pans a zoomed image. Not passive, to keep the page from zooming or scrolling.
  useEffect(() => {
    const s = stage.current;
    if (!s) return;
    const onWheel = (e: WheelEvent) => {
      const r = s.getBoundingClientRect();
      const px = e.clientX - r.left - r.width / 2, py = e.clientY - r.top - r.height / 2;
      if (e.ctrlKey || e.metaKey) {
        e.preventDefault();
        setView((v) => {
          const next = Math.min(MAX_SCALE, Math.max(1, v.scale * Math.exp(-e.deltaY * 0.01)));
          const k = next / v.scale;
          return clamp({ scale: next, x: px - (px - v.x) * k, y: py - (py - v.y) * k });
        });
      } else if (view.scale > 1) {
        e.preventDefault();
        setView((v) => clamp({ ...v, x: v.x - e.deltaX, y: v.y - e.deltaY }));
      }
    };
    s.addEventListener('wheel', onWheel, { passive: false });
    return () => s.removeEventListener('wheel', onWheel);
  }, [clamp, view.scale]);

  // Pointers: one drags (pans when zoomed, swipes otherwise), two pinch; a double tap or click
  // toggles the zoom. `moved` keeps the click that ends a drag from closing the viewer.
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const gesture = useRef<{ startX: number; startY: number; view: View; dist: number; moved: boolean; onBackdrop: boolean; pinched: boolean }>(
    { startX: 0, startY: 0, view: FIT, dist: 0, moved: false, onBackdrop: false, pinched: false });
  // The view as last rendered, for gestures that restart in the middle (a pinch losing a finger).
  const viewRef = useRef(view);
  viewRef.current = view;
  const lastTap = useRef({ time: 0, x: 0, y: 0 });

  const relative = (x: number, y: number) => {
    const r = stage.current!.getBoundingClientRect();
    return { px: x - r.left - r.width / 2, py: y - r.top - r.height / 2 };
  };
  const spread = () => {
    const [a, b] = [...pointers.current.values()];
    return a && b ? Math.hypot(a.x - b.x, a.y - b.y) : 0;
  };

  const onPointerDown = (e: ReactPointerEvent) => {
    if ((e.target as HTMLElement).closest('button')) return;
    // Pointer capture sends the click to the stage whatever was pressed, so remember it now.
    const onBackdrop = e.target === e.currentTarget;
    try { (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId); } catch { /* the pointer is already gone */ }
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    const pinched = pointers.current.size > 1 || gesture.current.pinched;
    gesture.current = { startX: e.clientX, startY: e.clientY, view, dist: spread(), moved: pinched, onBackdrop, pinched };
  };

  const onPointerMove = (e: ReactPointerEvent) => {
    if (!pointers.current.has(e.pointerId)) return;
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    const g = gesture.current;
    const dx = e.clientX - g.startX, dy = e.clientY - g.startY;
    if (Math.hypot(dx, dy) > 4) g.moved = true;
    if (pointers.current.size === 2 && g.dist > 0) {
      g.pinched = true;
      const [a, b] = [...pointers.current.values()];
      const { px, py } = relative((a!.x + b!.x) / 2, (a!.y + b!.y) / 2);
      const next = Math.min(MAX_SCALE, Math.max(1, g.view.scale * (spread() / g.dist)));
      const k = next / g.view.scale;
      setView(clamp({ scale: next, x: px - (px - g.view.x) * k, y: py - (py - g.view.y) * k }));
    } else if (pointers.current.size === 1 && g.view.scale > 1) {
      setView(clamp({ ...g.view, x: g.view.x + dx, y: g.view.y + dy }));
    }
  };

  const onPointerUp = (e: ReactPointerEvent) => {
    if (!pointers.current.has(e.pointerId)) return;
    pointers.current.delete(e.pointerId);
    const g = gesture.current;
    // After a pinch, the finger left on the screen carries on panning from where the pinch left the
    // image; lifting it neither swipes nor taps.
    if (g.pinched) {
      const rest = [...pointers.current.values()][0];
      if (rest) gesture.current = { ...g, startX: rest.x, startY: rest.y, view: viewRef.current, dist: 0, moved: true };
      else gesture.current = { ...g, pinched: false };
      return;
    }
    const dx = e.clientX - g.startX, dy = e.clientY - g.startY;
    if (g.view.scale === 1 && Math.abs(dx) > SWIPE_PX && Math.abs(dx) > Math.abs(dy)) {
      go(dx < 0 ? 1 : -1);
      return;
    }
    if (g.moved) return;
    const now = performance.now();
    const tap = lastTap.current;
    if (now - tap.time < DOUBLE_TAP_MS && Math.hypot(e.clientX - tap.x, e.clientY - tap.y) < 30) {
      const { px, py } = relative(e.clientX, e.clientY);
      if (view.scale > 1) setView(FIT);
      else zoomAt(2.5, px, py);
      lastTap.current = { time: 0, x: 0, y: 0 };
      g.moved = true; // not a click on the backdrop
      return;
    }
    lastTap.current = { time: now, x: e.clientX, y: e.clientY };
  };

  // A click on the dark backdrop closes; on the image (to zoom or pan) it never does.
  const onStageClick = () => {
    if (gesture.current.onBackdrop && !gesture.current.moved) close();
  };

  return (
    <div ref={root} className={`lightbox ${closing ? 'closing' : ''}`} role="dialog" aria-modal="true" aria-label={label ?? t('viewer.label')}
      // Escape in a field of the footer (the caption) belongs to that field: never let it reach the
      // sheet behind, which would close.
      onKeyDown={(e) => { if (e.key === 'Escape') e.stopPropagation(); }}>
      <div className="lightbox-bar">
        <span className="lightbox-count">{many ? `${index + 1} / ${images.length}` : ''}</span>
        <span className="spacer" />
        <button className="lightbox-button" onClick={() => zoomAt(view.scale / STEP)} disabled={view.scale <= 1} aria-label={t('viewer.zoomOut')} title={t('viewer.zoomOut')}><Icon name="zoomOut" size={20} /></button>
        <button className="lightbox-button" onClick={() => setView(FIT)} disabled={view.scale === 1} aria-label={t('viewer.resetZoom')} title={t('viewer.resetZoom')}><span className="lightbox-zoom">{Math.round(view.scale * 100)}%</span></button>
        <button className="lightbox-button" onClick={() => zoomAt(view.scale * STEP)} disabled={view.scale >= MAX_SCALE} aria-label={t('viewer.zoomIn')} title={t('viewer.zoomIn')}><Icon name="zoomIn" size={20} /></button>
        {canFullscreen && (
          <button className="lightbox-button" onClick={toggleFullscreen} aria-label={t(fullscreen ? 'viewer.exitFullscreen' : 'viewer.fullscreen')} title={t(fullscreen ? 'viewer.exitFullscreen' : 'viewer.fullscreen')}>
            <Icon name={fullscreen ? 'shrink' : 'expand'} size={20} />
          </button>
        )}
        <button ref={closeButton} className="lightbox-button" onClick={close} aria-label={t('viewer.close')} title={t('viewer.close')}><Icon name="close" size={22} /></button>
      </div>
      <div ref={stage} className={`lightbox-stage ${view.scale > 1 ? 'zoomed' : ''}`} onClick={onStageClick}
        onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={onPointerUp}>
        <img ref={img} src={images[index]} alt="" draggable={false}
          style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }} />
        {many && (
          <>
            <button className="lightbox-nav prev" onClick={() => go(-1)} aria-label={t('viewer.previous')} title={t('viewer.previous')}><Icon name="prev" size={28} /></button>
            <button className="lightbox-nav next" onClick={() => go(1)} aria-label={t('viewer.next')} title={t('viewer.next')}><Icon name="next" size={28} /></button>
          </>
        )}
      </div>
      {footer && <div className="lightbox-footer">{footer}</div>}
    </div>
  );
}
