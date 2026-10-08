import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { coverClient, errorMessage, proxiedImage } from '../../api/client';
import { Alert, Modal } from '../../components/ui';
import type { CoverCandidate } from '../../gen/gamevault/v1/provider_pb';

/**
 * "Choose cover", Plex style: shows what every enabled provider proposes. Picking one pins it as
 * the game's cover URL; "Automatic" clears the pin and lets the provider chain decide.
 */
export default function CoverPicker({ gameId, current, onPick, onClose }: {
  gameId: string;
  current: string;
  onPick: (url: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [candidates, setCandidates] = useState<CoverCandidate[] | null>(null);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [error, setError] = useState('');

  useEffect(() => {
    coverClient.listCoverCandidates({ gameId })
      .then((res) => { setCandidates(res.candidates); setWarnings(res.warnings); })
      .catch((e) => setError(errorMessage(e)));
  }, [gameId]);

  return (
    <Modal wide title={t('coverPicker.title')} onClose={onClose}>
      <p className="muted small">{t('coverPicker.intro')}</p>
      {error && <Alert tone="error">{error}</Alert>}
      {warnings.map((w) => <Alert key={w} tone="warn">{w}</Alert>)}
      {candidates === null && !error && <p className="muted">{t('common.loading')}</p>}
      {candidates && (
        <div className="posters picker-grid">
          <button className={`poster ${current === '' ? 'selected' : ''}`} onClick={() => onPick('')}>
            <div className="cover"><div className="cover-placeholder"><span>AUTO</span></div></div>
            <div className="poster-caption">
              <span className="poster-title">{t('coverPicker.automatic')}</span>
              <span className="muted small">{t('coverPicker.automaticHint')}</span>
            </div>
          </button>
          {candidates.map((c) => (
            <button key={c.url} className={`poster ${current === c.url ? 'selected' : ''}`} onClick={() => onPick(c.url)} title={c.label}>
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
