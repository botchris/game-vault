import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { coverClient, errorMessage, proxiedImage } from '../../api/client';
import { Alert, Modal } from '../../components/ui';
import type { Game } from '../../gen/gamevault/v1/game_pb';
import type { CoverCandidate } from '../../gen/gamevault/v1/provider_pb';

/** What the picker chose for the edition's cover (SetEditionCoverRequest.cover). */
export type CoverChoice = { case: 'url'; value: string } | { case: 'photoId'; value: string } | { case: 'clear'; value: true };

/**
 * "Choose cover", Plex style: shows what every enabled provider proposes for one edition of the
 * game. Picking one pins it as that edition's cover; "Automatic" clears the pin and lets the
 * provider chain decide.
 */
export default function CoverPicker({ game, system, onPick, onClose }: {
  game: Game;
  /** The edition whose cover is chosen ('' for a game without copies). */
  system: string;
  onPick: (choice: CoverChoice) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const edition = game.editions.find((e) => e.system === system);
  // The pinned URL ('' for Automatic), or null when a photo of a copy is the cover.
  const current = edition?.coverPhotoId ? null : edition?.coverUrl ?? '';
  const [candidates, setCandidates] = useState<CoverCandidate[] | null>(null);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [error, setError] = useState('');

  useEffect(() => {
    coverClient.listCoverCandidates({ gameId: game.id, system })
      .then((res) => { setCandidates(res.candidates); setWarnings(res.warnings); })
      .catch((e) => setError(errorMessage(e)));
  }, [game.id, system]);

  return (
    <Modal wide title={t('coverPicker.title')} onClose={onClose}>
      <p className="muted small">{t('coverPicker.intro')}</p>
      {error && <Alert tone="error">{error}</Alert>}
      {warnings.map((w) => <Alert key={w} tone="warn">{w}</Alert>)}
      {candidates === null && !error && <p className="muted">{t('common.loading')}</p>}
      {candidates && (
        <div className="posters picker-grid">
          <button className={`poster ${current === '' ? 'selected' : ''}`} onClick={() => onPick({ case: 'clear', value: true })}>
            <div className="cover"><div className="cover-placeholder"><span>AUTO</span></div></div>
            <div className="poster-caption">
              <span className="poster-title">{t('coverPicker.automatic')}</span>
              <span className="muted small">{t('coverPicker.automaticHint')}</span>
            </div>
          </button>
          {candidates.map((c) => (
            <button key={c.url} className={`poster ${current === c.url ? 'selected' : ''}`} onClick={() => onPick({ case: 'url', value: c.url })} title={c.label}>
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
    </Modal>
  );
}
