import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal } from '../../components/ui';
import { useAppData } from '../../state/AppData';

interface Props {
  title: string;
  excludeId: string;
  /** When set, offers creating a new game with the typed title instead of picking one. */
  allowNew?: boolean;
  onPick: (target: { id: string } | { newTitle: string }) => void;
  onClose: () => void;
}

/** Search-and-pick dialog used to merge games and to move copies between games. */
export default function GamePicker({ title, excludeId, allowNew, onPick, onClose }: Props) {
  const { t } = useTranslation();
  const { games } = useAppData();
  const [q, setQ] = useState('');

  const matches = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return games.filter((g) => g.id !== excludeId && (!needle || g.title.toLowerCase().includes(needle))).slice(0, 50);
  }, [games, q, excludeId]);

  return (
    <Modal title={title} onClose={onClose}>
      <input className="search full" type="search" autoFocus placeholder={t('picker.search')} value={q} onChange={(e) => setQ(e.target.value)} />
      <ul className="picker">
        {allowNew && q.trim() && (
          <li><button onClick={() => onPick({ newTitle: q.trim() })}>+ {t('picker.newGame', { title: q.trim() })}</button></li>
        )}
        {matches.map((g) => (
          <li key={g.id}>
            <button onClick={() => onPick({ id: g.id })}>
              {g.title} <span className="muted small">· {t('picker.copies', { count: g.copies.length })}</span>
            </button>
          </li>
        ))}
        {matches.length === 0 && <li className="muted">{t('picker.noMatches')}</li>}
      </ul>
    </Modal>
  );
}
