import { useRef, useState, type PointerEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, fieldClient } from '../../api/client';
import { Icon } from '../../components/Icon';
import { Alert } from '../../components/ui';
import type { FieldDefinition } from '../../gen/gamevault/v1/field_pb';
import { KINDS, kindKey } from '../../lib/model';
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
  const [drag, setDrag] = useState<Drag | null>(null);
  // The order a drop asked for, shown while MoveField runs so the row does not jump back meanwhile.
  const [pending, setPending] = useState<string[] | null>(null);
  const rows = useRef(new Map<string, HTMLLIElement>());
  const shown = pending ? pending.flatMap((id) => fields.filter((f) => f.id === id)) : fields;

  const move = async (f: FieldDefinition, index: number) => {
    setBusy(true);
    setError('');
    try {
      await fieldClient.moveField({ id: f.id, index });
      await reloadFields();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setPending(null);
      setBusy(false);
    }
  };

  // Drag to reorder (mouse and pen on desktop; the arrows do it everywhere, keyboard included). The
  // row follows the pointer and the rows it passes slide out of its way; the drop calls MoveField.
  const startDrag = (e: PointerEvent<HTMLElement>, f: FieldDefinition, index: number) => {
    if (busy || e.button !== 0) return;
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    const rects = shown.map((x) => rows.current.get(x.id)!.getBoundingClientRect());
    const gap = rects.length > 1 ? rects[1]!.top - rects[0]!.bottom : 0;
    setDrag({ id: f.id, from: index, to: index, dy: 0, startY: e.clientY, mids: rects.map((r) => r.top + r.height / 2), step: rects[index]!.height + gap });
  };
  const moveDrag = (e: PointerEvent<HTMLElement>) => {
    if (!drag) return;
    const dy = e.clientY - drag.startY;
    setDrag({ ...drag, dy, to: dropIndex(drag, drag.mids[drag.from]! + dy) });
  };
  const endDrag = (drop: boolean) => {
    if (!drag) return;
    setDrag(null);
    if (!drop || drag.to === drag.from) return;
    const ids = shown.map((f) => f.id);
    ids.splice(drag.to, 0, ...ids.splice(drag.from, 1));
    setPending(ids);
    void move(shown[drag.from]!, drag.to);
  };

  const scopeLabel = (f: FieldDefinition) => {
    if (f.scope !== 'copy') return t('fields.scopeGame');
    const kinds = f.kinds.length ? KINDS.filter((k) => f.kinds.includes(k)).map((k) => t(`kind.${kindKey(k)}`)).join(', ') : t('fields.kindsAll');
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
        <ol className={`chain${drag ? ' dragging' : ''}`}>
          {shown.map((f, i) => (
            <li key={f.id} className={`chain-item${drag?.id === f.id ? ' lifted' : ''}`} style={drag ? { transform: shift(drag, i) } : undefined}
              ref={(el) => { if (el) rows.current.set(f.id, el); else rows.current.delete(f.id); }}>
              <span className="chain-grip" title={t('fields.dragToReorder')} aria-hidden="true"
                onPointerDown={(e) => startDrag(e, f, i)} onPointerMove={moveDrag}
                onPointerUp={() => endDrag(true)} onPointerCancel={() => endDrag(false)}>
                <Icon name="grip" size={18} />
              </span>
              <div className="chain-body">
                <strong>{f.name}</strong>
                <span className="muted small">{t(`fields.types.${f.type}`)} · {scopeLabel(f)}</span>
              </div>
              <div className="chain-actions">
                <button className="icon" title={t('fields.moveUp')} aria-label={t('fields.moveUp')} disabled={busy || i === 0} onClick={() => move(f, i - 1)}>▲</button>
                <button className="icon" title={t('fields.moveDown')} aria-label={t('fields.moveDown')} disabled={busy || i === shown.length - 1} onClick={() => move(f, i + 1)}>▼</button>
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

/** A drag in progress: the row's index when it started and where it would drop. */
interface Drag {
  id: string;
  from: number;
  to: number;
  /** How far the pointer moved since the press, in px. */
  dy: number;
  startY: number;
  /** The vertical middle of every row when the drag started. */
  mids: number[];
  /** How far the other rows slide to make room: the dragged row's height plus the gap. */
  step: number;
}

/** Where a row whose middle is at y drops: past every middle it crossed, counted from its start. */
function dropIndex(drag: Drag, y: number): number {
  let to = drag.from;
  drag.mids.forEach((mid, j) => {
    if (j > drag.from && y > mid) to = j;
    if (j < drag.from && y < mid && j < to) to = j;
  });
  return to;
}

/** The transform of row i while dragging: the dragged row follows the pointer, the others make room. */
function shift(drag: Drag, i: number): string {
  if (i === drag.from) return `translateY(${drag.dy}px)`;
  if (drag.from < drag.to && i > drag.from && i <= drag.to) return `translateY(${-drag.step}px)`;
  if (drag.to < drag.from && i >= drag.to && i < drag.from) return `translateY(${drag.step}px)`;
  return 'translateY(0)';
}
