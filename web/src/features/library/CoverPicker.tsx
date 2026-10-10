import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { coverClient, errorMessage, photoUrl, proxiedImage } from '../../api/client';
import { Alert, Modal } from '../../components/ui';
import type { Game } from '../../gen/gamevault/v1/game_pb';
import type { CoverCandidate } from '../../gen/gamevault/v1/provider_pb';

/** What the picker chose for the edition's cover (SetEditionCoverRequest.cover). */
export type CoverChoice = { case: 'url'; value: string } | { case: 'photoId'; value: string } | { case: 'clear'; value: true };

/**
 * "Choose cover", Plex style, for one edition of the game: the photos of the edition's copies, then
 * what every enabled provider proposes for its system, then an image address to paste. Picking one
 * pins it as that edition's cover; "Automatic" clears the pin and lets the provider chain decide.
 */
export default function CoverPicker({ game, system, photosOnly = false, onPick, onClose }: {
  game: Game;
  /** The edition whose cover is chosen ('' for a game without copies). */
  system: string;
  /** Show only the photos of the edition's copies ("Use a photo"). */
  photosOnly?: boolean;
  onPick: (choice: CoverChoice) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const edition = game.editions.find((e) => e.system === system);
  // The photos of the edition's copies, each once (copies may share one).
  const photos = game.copies.filter((c) => c.effectiveSystem === system).flatMap((c) => c.photos)
    .filter((p, i, all) => all.findIndex((x) => x.id === p.id) === i);
  const [candidates, setCandidates] = useState<CoverCandidate[] | null>(photosOnly ? [] : null);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [error, setError] = useState('');
  const [pasted, setPasted] = useState('');
  const automatic = !edition?.coverUrl && !edition?.coverPhotoId;

  useEffect(() => {
    if (photosOnly) return;
    coverClient.listCoverCandidates({ gameId: game.id, system })
      .then((res) => { setCandidates(res.candidates); setWarnings(res.warnings); })
      .catch((e) => setError(errorMessage(e)));
  }, [game.id, system, photosOnly]);

  return (
    <Modal wide title={system ? t('coverPicker.titleFor', { system }) : t('coverPicker.title')} onClose={onClose}>
      {photos.length > 0 && (
        <>
          <h3>{t('coverPicker.photos')}</h3>
          <div className="posters picker-grid">
            {photos.map((p) => (
              <button key={p.id} className={`poster ${edition?.coverPhotoId === p.id ? 'selected' : ''}`} title={p.caption}
                onClick={() => onPick({ case: 'photoId', value: p.id })}>
                <div className="cover"><img src={photoUrl(p.id, true)} alt={p.caption} loading="lazy" /></div>
              </button>
            ))}
          </div>
        </>
      )}
      {!photosOnly && (
        <>
          <p className="muted small">{t('coverPicker.intro')}</p>
          {error && <Alert tone="error">{error}</Alert>}
          {warnings.map((w) => <Alert key={w} tone="warn">{w}</Alert>)}
          {candidates === null && !error && <p className="muted">{t('common.loading')}</p>}
          {candidates && (
            <div className="posters picker-grid">
              <button className={`poster ${automatic ? 'selected' : ''}`} onClick={() => onPick({ case: 'clear', value: true })}>
                <div className="cover"><div className="cover-placeholder"><span>AUTO</span></div></div>
                <div className="poster-caption">
                  <span className="poster-title">{t('coverPicker.automatic')}</span>
                  <span className="muted small">{t('coverPicker.automaticHint')}</span>
                </div>
              </button>
              {candidates.map((c) => (
                <button key={c.url} className={`poster ${edition?.coverUrl === c.url ? 'selected' : ''}`} onClick={() => onPick({ case: 'url', value: c.url })} title={c.label}>
                  <div className="cover"><img src={proxiedImage(c.thumbUrl || c.url)} alt="" loading="lazy" /></div>
                  <div className="poster-caption">
                    <span className="poster-title">{c.label || c.providerName}</span>
                    <span className="muted small">{c.providerName}</span>
                  </div>
                </button>
              ))}
            </div>
          )}
          {candidates?.length === 0 && <p className="muted">{t('coverPicker.none')}</p>}
          <form className="row tight cover-paste" onSubmit={(e) => { e.preventDefault(); if (pasted.trim()) onPick({ case: 'url', value: pasted.trim() }); }}>
            <label>
              {t('game.coverUrl')}
              <input type="url" value={pasted} placeholder="https://…" onChange={(e) => setPasted(e.target.value)} />
            </label>
            <button type="submit" disabled={!pasted.trim()}>{t('coverPicker.paste')}</button>
          </form>
        </>
      )}
    </Modal>
  );
}
