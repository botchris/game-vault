import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, gameClient, proxiedImage } from '../../api/client';
import { Alert, Modal } from '../../components/ui';
import type { SteamApp } from '../../gen/gamevault/v1/game_pb';

/** Searches the Steam store to link a game to its AppID (which also gives it a cover). */
export default function SteamSearchDialog({ initialQuery, onPick, onClose }: {
  initialQuery: string;
  onPick: (app: SteamApp) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [q, setQ] = useState(initialQuery);
  const [apps, setApps] = useState<SteamApp[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    const query = q.trim();
    if (query.length < 2) return;
    const timer = setTimeout(async () => {
      setLoading(true);
      try {
        setApps((await gameClient.searchSteamApps({ query })).apps);
        setError('');
      } catch (e) {
        setError(errorMessage(e));
      } finally {
        setLoading(false);
      }
    }, 300); // debounce typing
    return () => clearTimeout(timer);
  }, [q]);

  return (
    <Modal title={t('steamSearch.title')} onClose={onClose}>
      <input className="search full" type="search" autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('steamSearch.placeholder')} />
      {error && <Alert tone="error">{error}</Alert>}
      <ul className="picker">
        {loading && <li className="muted">{t('common.loading')}</li>}
        {!loading && apps.length === 0 && q.trim().length >= 2 && <li className="muted">{t('picker.noMatches')}</li>}
        {apps.map((a) => (
          <li key={a.appId.toString()}>
            <button onClick={() => onPick(a)}>
              {a.imageUrl && <img className="steam-thumb" src={proxiedImage(a.imageUrl)} alt="" loading="lazy" />}
              <span>{a.name} <span className="muted small">· AppID {a.appId.toString()}</span></span>
            </button>
          </li>
        ))}
      </ul>
    </Modal>
  );
}
