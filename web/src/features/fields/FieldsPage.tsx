import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, fieldClient } from '../../api/client';
import { Icon } from '../../components/Icon';
import { Alert } from '../../components/ui';
import type { FieldDefinition } from '../../gen/gamevault/v1/field_pb';
import { kindKey } from '../../lib/model';
import { useAppData } from '../../state/AppData';
import FieldDialog from './FieldDialog';

/** Custom fields: the user's own fields on games and copies, in the order the game sheet shows them. */
export default function FieldsPage() {
  const { t } = useTranslation();
  const { fields, reloadFields } = useAppData();
  // null: closed; 'new': adding; a definition: editing it.
  const [editing, setEditing] = useState<FieldDefinition | 'new' | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const move = async (f: FieldDefinition, index: number) => {
    setBusy(true);
    setError('');
    try {
      await fieldClient.moveField({ id: f.id, index });
      await reloadFields();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const scopeLabel = (f: FieldDefinition) => {
    if (f.scope !== 'copy') return t('fields.scopeGame');
    const kinds = f.kinds.length ? [...f.kinds].sort().map((k) => t(`kind.${kindKey(k)}`)).join(', ') : t('fields.kindsAll');
    return `${t('fields.scopeCopy')} (${kinds})`;
  };

  return (
    <div className="page fields">
      <header className="page-head">
        <h1 className="page-title">{t('fields.title')}</h1>
        <button className="primary" onClick={() => setEditing('new')}><Icon name="plus" size={18} /> {t('fields.add')}</button>
      </header>
      <p className="muted fields-intro">{t('fields.intro')}</p>
      {error && <Alert tone="error">{error}</Alert>}

      {fields.length === 0 ? (
        <div className="empty"><p>{t('fields.empty')}</p></div>
      ) : (
        <ol className="chain">
          {fields.map((f, i) => (
            <li key={f.id} className="chain-item">
              <div className="chain-body">
                <strong>{f.name}</strong>
                <span className="muted small">{t(`fields.types.${f.type}`)} · {scopeLabel(f)}</span>
              </div>
              <div className="chain-actions">
                <button className="icon" title={t('fields.moveUp')} aria-label={t('fields.moveUp')} disabled={busy || i === 0} onClick={() => move(f, i - 1)}>▲</button>
                <button className="icon" title={t('fields.moveDown')} aria-label={t('fields.moveDown')} disabled={busy || i === fields.length - 1} onClick={() => move(f, i + 1)}>▼</button>
                <button onClick={() => setEditing(f)}>{t('common.edit')}</button>
              </div>
            </li>
          ))}
        </ol>
      )}

      {editing && (
        <FieldDialog field={editing === 'new' ? undefined : editing} onClose={() => setEditing(null)} />
      )}
    </div>
  );
}
