import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, gameClient, proxiedImage } from '../../api/client';
import { Alert, Modal } from '../../components/ui';
import type { LinkMatch, LinkStore } from '../../gen/gamevault/v1/game_pb';

/** Searches a store's catalog to link a game to it (which also gives it a cover and details). */
export default function LinkSearchDialog({ store, initialQuery, onPick, onClose }: {
  store: LinkStore;
  initialQuery: string;
  onPick: (match: LinkMatch) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [q, setQ] = useState(initialQuery);
  const [matches, setMatches] = useState<LinkMatch[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    const query = q.trim();
    if (query.length < 2) return;
    const timer = setTimeout(async () => {
      setLoading(true);
      try {
        setMatches((await gameClient.searchLinks({ store: store.key, query })).matches);
        setError('');
      } catch (e) {
        setError(errorMessage(e));
      } finally {
        setLoading(false);
      }
    }, 300); // debounce typing
    return () => clearTimeout(timer);
  }, [q, store.key]);

  return (
    <Modal title={t('linkSearch.title', { store: store.name })} onClose={onClose}>
      <input className="search full" type="search" autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('linkSearch.placeholder')} />
      {error && <Alert tone="error">{error}</Alert>}
      <ul className="picker">
        {loading && <li className="muted">{t('common.loading')}</li>}
        {!loading && matches.length === 0 && q.trim().length >= 2 && <li className="muted">{t('picker.noMatches')}</li>}
        {matches.map((m) => (
          <li key={m.id}>
            <button onClick={() => onPick(m)}>
              {m.imageUrl && <img className="link-thumb" src={proxiedImage(m.imageUrl)} alt="" loading="lazy" />}
              <span>{m.name} <span className="muted small">· {store.name} {m.id}</span></span>
            </button>
          </li>
        ))}
      </ul>
    </Modal>
  );
}
