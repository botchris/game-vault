import { useCallback, useEffect, useState, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { notificationClient } from '../../api/client';
import { Icon, type IconName } from '../../components/Icon';
import { PageHeader } from '../../components/PageHeader';
import { FieldHelp } from '../../components/FieldHelp';
import { Alert, Modal } from '../../components/ui';
import type { Connection, ConnectorType, Delivery, ListConnectorTypesResponse } from '../../gen/gamevault/v1/notify_pb';
import { connectionState, type ConnectionState } from '../../lib/notifications';
import { useAction } from '../../lib/useAction';
import { replaceRoute } from '../../lib/useHashRoute';
import { useHealth } from '../../state/Health';
import ConnectionDialog from './ConnectionDialog';

/** The icon of each connector type; a type this version does not know gets the bell. */
const TYPE_ICONS: Record<string, IconName> = { telegram: 'send', ntfy: 'bell', webhook: 'link' };

/** The icon of a connector type. */
export function typeIcon(type: string): IconName {
  return TYPE_ICONS[type] ?? 'bell';
}

/** Settings → Notifications: the address messages link to, the connections (Telegram, ntfy, a
 *  webhook) with their switch and state, and adding one. */
export default function NotificationsPage({ target }: {
  /** The connection whose dialog the address opens ("#/notifications/<id>", a health issue's link), if any. */
  target: string;
}) {
  const { t } = useTranslation();
  const [catalog, setCatalog] = useState<ListConnectorTypesResponse | null>(null);
  const [connections, setConnections] = useState<Connection[]>([]);
  // The newest delivery of each connection, which tells whether it ever sent anything.
  const [latest, setLatest] = useState<Record<string, Delivery | undefined>>({});
  const [editing, setEditing] = useState<{ type: ConnectorType; connection?: Connection } | null>(null);
  const [picking, setPicking] = useState(false);
  const { error, run } = useAction();
  const toggling = useAction();
  const { reload: reloadHealth } = useHealth();

  const loadConnections = useCallback(async () => {
    const { connections: list } = await notificationClient.listConnections({});
    const lasts = await Promise.all(list.map((c) => notificationClient.listDeliveries({ connectionId: c.id })
      .then((r) => r.deliveries[0], () => undefined)));
    setConnections(list);
    setLatest(Object.fromEntries(list.map((c, i) => [c.id, lasts[i]])));
  }, []);

  useEffect(() => {
    void run(async () => {
      const [types] = await Promise.all([notificationClient.listConnectorTypes({}), loadConnections()]);
      setCatalog(types);
    });
  }, [run, loadConnections]);

  const typeOf = (c: Connection) => catalog?.types.find((x) => x.id === c.type);

  // "#/notifications/<id>" opens that connection's dialog, once: the address goes back to the page,
  // so closing the dialog and reloading do not open it again.
  useEffect(() => {
    const c = target ? connections.find((x) => x.id === target) : undefined;
    const ty = c && catalog?.types.find((x) => x.id === c.type);
    if (!c || !ty) return;
    setEditing({ type: ty, connection: c });
    replaceRoute('#/notifications');
  }, [target, connections, catalog]);

  // The switch saves the connection as it is with `enabled` flipped; masked secrets keep their values.
  const toggle = (c: Connection) => toggling.run(async () => {
    await notificationClient.saveConnection({
      id: c.id,
      connection: { type: c.type, name: c.name, enabled: !c.enabled, events: c.events, language: c.language, settings: c.settings },
    });
    await loadConnections();
    reloadHealth();
  });

  const afterChange = async () => {
    setEditing(null);
    await run(loadConnections);
    reloadHealth();
  };

  return (
    <div className="page sources notifications">
      <PageHeader title={t('notifications.title')} actions={catalog && catalog.types.length > 0 && (
        <button className="primary" onClick={() => setPicking(true)}>{t('notifications.add')}</button>
      )} />
      <p className="muted sources-intro">{t('notifications.intro')}</p>

      {error && <Alert tone="error">{error}</Alert>}
      {toggling.error && <Alert tone="error">{toggling.error}</Alert>}

      <AddressField />

      <h2 className="notify-heading">{t('notifications.connections')}</h2>
      {connections.length === 0 ? (
        catalog && <p className="muted">{t('notifications.none')}</p>
      ) : (
        <ul className="source-list">
          {connections.map((c) => (
            <ConnectionRow key={c.id} connection={c} typeName={typeOf(c)?.name ?? c.type}
              state={connectionState(c, latest[c.id])} busy={toggling.busy}
              onToggle={() => toggle(c)}
              onEdit={() => { const ty = typeOf(c); if (ty) setEditing({ type: ty, connection: c }); }} />
          ))}
        </ul>
      )}

      {picking && catalog && (
        <Modal title={t('notifications.chooseType')} onClose={() => setPicking(false)}>
          <ul className="source-tiles notify-types">
            {catalog.types.map((ty) => (
              <li key={ty.id}>
                <button className="source-tile" onClick={() => { setPicking(false); setEditing({ type: ty }); }}>
                  <span className="notify-icon"><Icon name={typeIcon(ty.id)} size={20} /></span>
                  <span className="source-tile-text">
                    <span className="source-tile-name">{ty.name}</span>
                    <span className="source-tile-tagline">{t(ty.descriptionKey)}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </Modal>
      )}

      {editing && catalog && (
        <ConnectionDialog type={editing.type} connection={editing.connection} catalog={catalog}
          onClose={() => setEditing(null)} onSaved={afterChange} onDeleted={afterChange} />
      )}
    </div>
  );
}

/** The Game Vault address messages link to: saved when the field is left or on Enter, the error
 *  under it. */
function AddressField() {
  const { t } = useTranslation();
  const [stored, setStored] = useState<string | null>(null);
  const [value, setValue] = useState('');
  const [saved, setSaved] = useState(false);
  const { busy, error, setError, run } = useAction();

  useEffect(() => {
    void run(async () => {
      const res = await notificationClient.getNotificationSettings({});
      setStored(res.address);
      setValue(res.address);
    });
  }, [run]);

  const save = async () => {
    if (stored === null || value.trim() === stored) {
      setError('');
      return;
    }
    setSaved(false);
    await run(async () => {
      const res = await notificationClient.saveNotificationSettings({ address: value.trim() });
      setStored(res.address);
      setValue(res.address);
      setSaved(true);
    });
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== 'Enter') return;
    e.preventDefault();
    void save();
  };

  return (
    <div className="field notify-address">
      <label htmlFor="notify-address">{t('notifications.address')}</label>
      <input id="notify-address" type="url" inputMode="url" autoComplete="off" placeholder="http://192.168.1.10:8080"
        value={value} disabled={stored === null} readOnly={busy} aria-invalid={error ? true : undefined}
        onChange={(e) => { setValue(e.target.value); setSaved(false); }} onBlur={save} onKeyDown={onKeyDown} />
      {error && <p className="small danger notify-address-error" role="alert">{error}</p>}
      {saved && !error && <p className="small muted" role="status">{t('notifications.addressSaved')}</p>}
      <FieldHelp text={t('notifications.addressHelp')} />
    </div>
  );
}

const STATE_TONES: Record<ConnectionState, string> = { working: 'ok', failing: 'warn', paused: 'error', off: 'never', untested: 'never' };

function ConnectionRow(props: {
  connection: Connection;
  typeName: string;
  state: ConnectionState;
  busy: boolean;
  onToggle: () => void;
  onEdit: () => void;
}) {
  const { t } = useTranslation();
  const { connection: c, state } = props;
  const status = state === 'paused' ? `${t('notifications.state.paused')} · ${c.pausedReason}` : t(`notifications.state.${state}`);
  return (
    <li className={`source-row ${c.enabled ? '' : 'disabled'}`}>
      <span className="notify-icon"><Icon name={typeIcon(c.type)} size={20} /></span>
      <button className="source-main" onClick={props.onEdit}>
        <span className="source-name">
          {c.name}
          {c.name !== props.typeName && <span className="source-type"> · {props.typeName}</span>}
        </span>
        <span className={`source-status tone-${STATE_TONES[state]}`}>
          <span className="source-dot" aria-hidden="true" />
          <span className="source-status-text">{status}</span>
        </span>
      </button>
      <span className="source-meta">
        <span className="source-schedule">{t('notifications.eventCount', { count: c.events.length })}</span>
      </span>
      <label className="switch notify-switch">
        <input type="checkbox" checked={c.enabled} disabled={props.busy} onChange={props.onToggle} />
        <span className="sr-only">{t('notifications.toggle', { name: c.name })}</span>
      </label>
    </li>
  );
}
