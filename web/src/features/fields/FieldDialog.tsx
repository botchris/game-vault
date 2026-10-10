import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, fieldClient } from '../../api/client';
import { Alert, Modal } from '../../components/ui';
import type { FieldDefinition } from '../../gen/gamevault/v1/field_pb';
import { KINDS, kindKey, type CopyKind } from '../../lib/model';
import { usePreferredCurrency } from '../../lib/usePreferences';
import { useAppData } from '../../state/AppData';

/** The field types, in the order the type select offers them. */
const TYPES = ['text', 'longtext', 'bool', 'number', 'money', 'date', 'duration', 'list', 'multilist'] as const;

/** A list value being edited: key identifies the row, id is empty until the server assigns one. */
interface Row { key: number; id: string; name: string }

interface Props {
  field?: FieldDefinition;
  onClose: () => void;
}

/**
 * Create or edit a custom field. Type and scope are fixed once the field exists. Removing a list
 * value that is already saved happens at once (its games may move to another value), the rest of
 * the form is sent on Save.
 */
export default function FieldDialog({ field, onClose }: Props) {
  const { t, i18n } = useTranslation();
  const { reloadFields, reloadGames } = useAppData();
  const defaultCurrency = usePreferredCurrency(i18n.language);
  const [name, setName] = useState(field?.name ?? '');
  const [type, setType] = useState(field?.type ?? 'text');
  const [scope, setScope] = useState(field?.scope ?? 'game');
  const [kinds, setKinds] = useState<CopyKind[]>(field?.kinds ?? []);
  const [decimals, setDecimals] = useState(field?.decimals ?? 0);
  const [unit, setUnit] = useState(field?.unit ?? '');
  const [currency, setCurrency] = useState(field?.currency ?? '');
  const nextKey = useRef(0);
  const [rows, setRows] = useState<Row[]>(() => (field?.choices ?? []).map((c) => ({ key: nextKey.current++, id: c.id, name: c.name })));
  // The row that just got added takes the focus, so values can be typed one after another.
  const [focusKey, setFocusKey] = useState<number | null>(null);
  // The saved value being removed, waiting for "merge into".
  const [removing, setRemoving] = useState<{ key: number; mergeInto: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // Whether games or copies hold a value of the number field being edited: its decimals are then
  // fixed, because the stored values would be read at another scale. Other changes that do not fit
  // the stored values are refused by the server and shown as any save error.
  const [hasValues, setHasValues] = useState(false);
  const isList = type === 'list' || type === 'multilist';

  useEffect(() => {
    if (!field || field.type !== 'number') return;
    let live = true;
    fieldClient.fieldUsage({ id: field.id })
      .then((u) => { if (live) setHasValues(u.games + u.copies > 0); })
      .catch(() => {}); // the server still refuses the change; the hint is only a convenience
    return () => { live = false; };
  }, [field]);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError('');
    try {
      await fn();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const save = (e: FormEvent) => {
    e.preventDefault();
    run(async () => {
      const def = {
        id: field?.id ?? '',
        name,
        type,
        scope,
        kinds: scope === 'copy' ? kinds : [],
        decimals: type === 'number' ? decimals : 0,
        unit: type === 'number' ? unit : '',
        currency: type === 'money' ? currency : '',
        // A new row left empty is dropped; a saved one is kept so the server can say what is wrong.
        choices: isList ? rows.filter((r) => r.id || r.name.trim()).map((r) => ({ id: r.id, name: r.name })) : [],
      };
      if (field) await fieldClient.updateField({ field: def });
      else await fieldClient.createField({ field: def });
      await reloadFields();
      onClose();
    });
  };

  const remove = () => run(async () => {
    if (!field) return;
    const usage = await fieldClient.fieldUsage({ id: field.id });
    if (!confirm(t('fields.confirmDelete', { name: field.name, games: usage.games, copies: usage.copies }))) return;
    await fieldClient.deleteField({ id: field.id });
    await Promise.all([reloadFields(), reloadGames()]); // the games lose their values
    onClose();
  });

  const addRow = () => {
    const key = nextKey.current++;
    setRows((list) => [...list, { key, id: '', name: '' }]);
    setFocusKey(key);
  };

  const moveRow = (index: number, delta: number) => setRows((list) => {
    const next = [...list];
    const [row] = next.splice(index, 1);
    next.splice(index + delta, 0, row!);
    return next;
  });

  // A row not saved yet just goes; a saved value asks where its games go first.
  const startRemove = (row: Row) => {
    if (!row.id) {
      setRows((list) => list.filter((r) => r.key !== row.key));
      return;
    }
    setRemoving({ key: row.key, mergeInto: '' });
  };

  const confirmRemove = (row: Row) => run(async () => {
    if (!field || !removing) return;
    await fieldClient.removeChoice({ fieldId: field.id, choiceId: row.id, mergeInto: removing.mergeInto });
    setRows((list) => list.filter((r) => r.key !== row.key));
    setRemoving(null);
    await Promise.all([reloadFields(), reloadGames()]); // games with that value changed
  });

  // Enter in a value adds the next one instead of saving the whole form.
  const onValueKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== 'Enter') return;
    e.preventDefault();
    addRow();
  };

  const toggleKind = (k: CopyKind, on: boolean) => setKinds((list) => on ? [...list, k].sort() : list.filter((x) => x !== k));

  return (
    <Modal title={field ? t('fields.edit') : t('fields.add')} onClose={onClose}
      footer={<>
        {field && <button type="button" className="danger" onClick={remove} disabled={busy}>{t('fields.delete')}</button>}
        <span className="spacer" />
        <button type="button" onClick={onClose}>{t('common.cancel')}</button>
        <button type="submit" form="field-form" className="primary" disabled={busy}>{t('common.save')}</button>
      </>}>
      <form id="field-form" onSubmit={save}>
        <div className="grid">
          <label className="span2">
            {t('fields.name')}
            <input value={name} maxLength={60} required autoFocus={!field} onChange={(e) => setName(e.target.value)} />
          </label>
          <label>
            {t('fields.type')}
            <select value={type} disabled={!!field} onChange={(e) => setType(e.target.value)}>
              {TYPES.map((ty) => <option key={ty} value={ty}>{t(`fields.types.${ty}`)}</option>)}
            </select>
          </label>
          <label>
            {t('fields.scope')}
            <select value={scope} disabled={!!field} onChange={(e) => setScope(e.target.value)}>
              <option value="game">{t('fields.scopeGame')}</option>
              <option value="copy">{t('fields.scopeCopy')}</option>
            </select>
          </label>
          <p className="span2 muted small field-note">{t('fields.fixedAfterCreate')}</p>

          {scope === 'copy' && (
            <fieldset className="span2 field-kinds">
              <legend>{t('fields.kinds')}</legend>
              <div className="field-kinds-row">
                {KINDS.map((k) => (
                  <label key={k} className="check-inline">
                    <input type="checkbox" checked={kinds.includes(k)} onChange={(e) => toggleKind(k, e.target.checked)} />
                    {t(`kind.${kindKey(k)}`)}
                  </label>
                ))}
              </div>
              <p className="muted small">{t('fields.kindsHint')}</p>
            </fieldset>
          )}

          {type === 'number' && (
            <>
              <label>
                {t('fields.decimals')}
                <select value={decimals} disabled={hasValues} onChange={(e) => setDecimals(Number(e.target.value))}>
                  <option value={0}>0</option>
                  <option value={2}>2</option>
                </select>
                {hasValues && <span className="muted small">{t('fields.decimalsInUse')}</span>}
              </label>
              <label>
                {t('fields.unit')}
                <input value={unit} maxLength={10} placeholder={t('fields.unitPlaceholder')} onChange={(e) => setUnit(e.target.value)} />
              </label>
            </>
          )}

          {type === 'money' && (
            <label>
              {t('fields.currency')}
              <input value={currency} maxLength={3} placeholder={defaultCurrency} autoCapitalize="characters"
                onChange={(e) => setCurrency(e.target.value.toUpperCase())} />
              <span className="muted small">{t('fields.currencyDefault', { currency: defaultCurrency })}</span>
            </label>
          )}
        </div>

        {isList && (
          <section className="field-values">
            <h3>{t('fields.values')}</h3>
            {rows.length > 0 && (
              <ol>
                {rows.map((r, i) => (
                  <li key={r.key}>
                    <div className="field-value">
                      <input value={r.name} maxLength={60} aria-label={t('fields.values')} autoFocus={r.key === focusKey}
                        onKeyDown={onValueKey}
                        onChange={(e) => setRows((list) => list.map((x) => x.key === r.key ? { ...x, name: e.target.value } : x))} />
                      <div className="field-value-actions">
                        <button type="button" className="icon" title={t('fields.moveUp')} aria-label={t('fields.moveUp')} disabled={busy || i === 0} onClick={() => moveRow(i, -1)}>▲</button>
                        <button type="button" className="icon" title={t('fields.moveDown')} aria-label={t('fields.moveDown')} disabled={busy || i === rows.length - 1} onClick={() => moveRow(i, 1)}>▼</button>
                        <button type="button" className="small-button" disabled={busy || removing?.key === r.key} onClick={() => startRemove(r)}>{t('fields.removeValue')}</button>
                      </div>
                    </div>
                    {removing?.key === r.key && (
                      <div className="field-value-merge">
                        <label>
                          {t('fields.mergeInto')}
                          <select value={removing.mergeInto} onChange={(e) => setRemoving({ key: r.key, mergeInto: e.target.value })}>
                            <option value="">{t('fields.leaveEmpty')}</option>
                            {rows.filter((o) => o.id && o.key !== r.key).map((o) => <option key={o.key} value={o.id}>{o.name}</option>)}
                          </select>
                        </label>
                        <div className="field-value-actions">
                          <button type="button" className="small-button" onClick={() => setRemoving(null)} disabled={busy}>{t('common.cancel')}</button>
                          <button type="button" className="small-button danger" onClick={() => confirmRemove(r)} disabled={busy}>{t('fields.removeValue')}</button>
                        </div>
                      </div>
                    )}
                  </li>
                ))}
              </ol>
            )}
            <button type="button" className="small-button" onClick={addRow} disabled={busy}>{t('fields.addValue')}</button>
          </section>
        )}

        {error && <Alert tone="error">{error}</Alert>}
      </form>
    </Modal>
  );
}
