import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { mediaUrl } from '../api/client';
import type { Video } from '../gen/gamevault/v1/metadata_pb';

/** Trailers: Steam HLS streams (native in Safari, hls.js elsewhere) and YouTube embeds. */
export default function VideoPlayer({ videos }: { videos: Video[] }) {
  const { t } = useTranslation();
  const [index, setIndex] = useState(0);
  const video = videos[index];
  if (!video) return null;
  return (
    <div className="trailers">
      <div className="player">
        {video.youtubeId ? (
          <iframe key={video.youtubeId} src={`https://www.youtube-nocookie.com/embed/${encodeURIComponent(video.youtubeId)}`}
            title={video.title || t('details.trailer')} allow="encrypted-media; picture-in-picture; fullscreen" allowFullScreen />
        ) : (
          <HlsVideo key={video.hlsUrl} src={video.hlsUrl} poster={mediaUrl(video.thumbnailUrl)} />
        )}
      </div>
      {videos.length > 1 && (
        <div className="trailer-list">
          {videos.map((v, i) => (
            <button key={i} className={i === index ? 'active' : ''} onClick={() => setIndex(i)} title={v.title}>
              {v.thumbnailUrl ? <img src={mediaUrl(v.thumbnailUrl)} alt="" loading="lazy" /> : <span>▶</span>}
              <span className="small">{v.youtubeId ? 'YouTube' : v.title || t('details.trailer')}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function HlsVideo({ src, poster }: { src: string; poster: string }) {
  const ref = useRef<HTMLVideoElement>(null);
  const [error, setError] = useState(false);
  useEffect(() => {
    const el = ref.current!;
    if (el.canPlayType('application/vnd.apple.mpegurl')) {
      el.src = src; // Safari plays HLS natively
      return;
    }
    let destroyed = false;
    let hls: { destroy: () => void } | undefined;
    import('hls.js').then(({ default: Hls }) => {
      if (destroyed) return;
      if (!Hls.isSupported()) {
        setError(true);
        return;
      }
      const h = new Hls();
      h.loadSource(src);
      h.attachMedia(el);
      hls = h;
    }).catch(() => setError(true));
    return () => {
      destroyed = true;
      hls?.destroy();
    };
  }, [src]);
  return error ? <div className="muted small">⚠</div> : <video ref={ref} controls playsInline preload="none" poster={poster} />;
}
