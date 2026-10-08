import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, gameClient } from '../../api/client';
import { Alert, Modal } from '../../components/ui';
import { CopyKind, KINDS, emptyDetails, kindKey, PHYSICAL_PLATFORMS, STORE_PLATFORMS } from '../../lib/model';
import { useAppData } from '../../state/AppData';

/** Registers a new game together with its first copy. More copies are added from the game detail. */
export default function NewGameDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (id: string) => void }) {
  const { t } = useTranslation();
  const { putGame } = useAppData();
  const [title, setTitle] = useState('');
  const [kind, setKind] = useState<CopyKind>(CopyKind.PHYSICAL);
  const [platform, setPlatform] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const res = await gameClient.createGame({ title, copies: [{ ...emptyDetails(kind), platform }] });
      putGame(res.game!);
      onCreated(res.game!.id);
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  };

  return (
    <Modal title={t('library.addGame')} onClose={onClose}
      footer={<>
        <span className="spacer" />
        <button type="button" onClick={onClose}>{t('common.cancel')}</button>
        <button type="submit" form="new-game" className="primary" disabled={busy || !title.trim()}>{t('common.create')}</button>
      </>}>
      <form id="new-game" onSubmit={submit}>
        <div className="grid">
          <label className="span2">
            {t('game.title')}
            <input autoFocus required value={title} onChange={(e) => setTitle(e.target.value)} />
          </label>
          <label>
            {t('newGame.firstCopy')}
            <select value={kind} onChange={(e) => setKind(Number(e.target.value))}>
              {KINDS.map((k) => <option key={k} value={k}>{t(`kind.${kindKey(k)}`)}</option>)}
            </select>
          </label>
          <label>
            {t('copy.platform')}
            <input list="new-platforms" value={platform} onChange={(e) => setPlatform(e.target.value)} />
            <datalist id="new-platforms">
              {(kind === CopyKind.PHYSICAL ? PHYSICAL_PLATFORMS : STORE_PLATFORMS).map((p) => <option key={p} value={p} />)}
            </datalist>
          </label>
        </div>
        <p className="muted small">{t('newGame.hint')}</p>
        {error && <Alert tone="error">{error}</Alert>}
      </form>
    </Modal>
  );
}
