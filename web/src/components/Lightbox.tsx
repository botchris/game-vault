import { useEffect } from 'react';
import { mediaUrl } from '../api/client';

/** Full-screen image viewer with keyboard navigation. */
export default function Lightbox({ images, index, onIndex, onClose }: {
  images: string[];
  index: number;
  onIndex: (i: number) => void;
  onClose: () => void;
}) {
  useEffect(() => {
    // Capture phase + stopPropagation: Escape closes the lightbox, not the dialog behind it.
    const onKey = (e: KeyboardEvent) => {
      if (['Escape', 'ArrowRight', 'ArrowLeft'].includes(e.key)) e.stopPropagation();
      if (e.key === 'Escape') onClose();
      if (e.key === 'ArrowRight') onIndex((index + 1) % images.length);
      if (e.key === 'ArrowLeft') onIndex((index - 1 + images.length) % images.length);
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [index, images.length, onIndex, onClose]);
  return (
    <div className="lightbox" onClick={onClose}>
      <img src={mediaUrl(images[index]!)} alt="" onClick={(e) => { e.stopPropagation(); onIndex((index + 1) % images.length); }} />
      <span className="lightbox-count">{index + 1} / {images.length}</span>
    </div>
  );
}
