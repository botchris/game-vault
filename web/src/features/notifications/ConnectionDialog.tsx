import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { notificationClient } from '../../api/client';
import { SettingInputs, SettingsResultAlert, TestButton, useSettingsTest } from '../../components/SettingsForm';
import { Alert, Modal } from '../../components/ui';
import type { Connection, ConnectorType, ListConnectorTypesResponse } from '../../gen/gamevault/v1/notify_pb';
import { LANGUAGES } from '../../i18n';
import { groupEvents, toggleGroup } from '../../lib/notifications';
import { useAction } from '../../lib/useAction';
import Deliveries from './Deliveries';

interface Props {
  type: ConnectorType;
  connection?: Connection;
  /** The connector types' answer: the events, languages and secret placeholder. */
  catalog: ListConnectorTypesResponse;
  onClose: () => void;
  onSaved: () => void;
  onDeleted: () => void;
}

/** Create or edit a connection: its settings, language and events, a test, and (when editing) its
 *  recent deliveries. Secret settings come back masked; leaving the mask keeps the stored value. */
export default function ConnectionDialog({ type, connection, catalog, onClose, onSaved, onDeleted }: Props) {
  const { t, i18n } = useTranslation();
  const [name, setName] = useState(connection?.name ?? type.name);
  const [enabled, setEnabled] = useState(connection?.enabled ?? true);
  // A new connection starts with every event: removing the few unwanted is quicker than finding them.
  const [events, setEvents] = useState<string[]>(() => connection?.events ?? catalog.events.map((e) => e.kind));
  const [language, setLanguage] = useState(() => connection?.language
    || (catalog.languages.includes(i18n.language) ? i18n.language : 'en'));
  const [settings, setSettings] = useState<Record<string, string>>(() => ({ ...(connection?.settings ?? {}) }));
  // What the last test found and filled in (Telegram's chat), shown under the result.
  const [found, setFound] = useState<string[]>([]);

  const input = (with_ = settings) => ({ type: type.id, name, enabled, events, language, settings: with_ });
  const { testing, result, setResult, test } = useSettingsTest(async (with_) => {
    setFound([]);
    const res = await notificationClient.testConnection({ id: connection?.id ?? '', connection: input(with_) });
    const values = Object.values(res.found);
    if (values.length > 0) {
      setSettings((s) => ({ ...s, ...res.found }));
      setFound(values);
    }
    return res;
  });
  // Saving and deleting close the dialog on success, so it stays busy until it goes.
  const action = useAction({ keepBusyOnSuccess: true, onError: (text) => setResult({ tone: 'error', text }) });
  const busy = testing || action.busy;
  const noEvents = events.length === 0;

  const save = (e: FormEvent) => {
    e.preventDefault();
    void action.run(async () => {
      await notificationClient.saveConnection({ id: connection?.id ?? '', connection: input() });
      onSaved();
    });
  };

  const remove = async () => {
    if (!connection || !confirm(t('notifications.deleteConfirm', { name: connection.name }))) return;
    await action.run(async () => {
      await notificationClient.deleteConnection({ id: connection.id });
      onDeleted();
    });
  };

  const languageName = (code: string) => LANGUAGES.find((l) => l.code === code)?.name ?? code;

  return (
    <Modal title={connection ? t('notifications.editTitle', { name: connection.name }) : t('notifications.addTitle', { type: type.name })}
      onClose={onClose} phoneFull
      footer={<>
        {connection && <button type="button" className="danger" onClick={remove} disabled={busy}>{t('common.delete')}</button>}
        <TestButton testing={testing} disabled={busy} onClick={() => test(settings)} />
        <span className="spacer" />
        <button type="button" onClick={onClose}>{t('common.cancel')}</button>
        <button type="submit" form="connection-form" className="primary" disabled={busy || noEvents}>{t('common.save')}</button>
      </>}>
      <form id="connection-form" onSubmit={save}>
        <p className="muted">{t(type.descriptionKey)}</p>
        {connection?.pausedReason && <Alert tone="error">{t('notifications.pausedNote', { reason: connection.pausedReason })}</Alert>}
        <div className="grid">
          <label className="span2">
            {t('notifications.name')}
            <input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          <SettingInputs fields={type.fields} settings={settings} onChange={(next) => { setFound([]); setSettings(next); }} idPrefix="connection" />
          <label>
            {t('notifications.language')}
            <select value={language} onChange={(e) => setLanguage(e.target.value)}>
              {catalog.languages.map((l) => <option key={l} value={l}>{languageName(l)}</option>)}
            </select>
          </label>
          <label className="check">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            {t('notifications.enabled')}
          </label>
        </div>
        <EventPicker catalog={catalog} selected={events} onChange={setEvents} />
        {noEvents && <p className="small danger">{t('notifications.noEvents')}</p>}
        <p className="muted small">{t('notifications.secretsNote')}</p>
        <SettingsResultAlert result={result} />
        {found.length > 0 && <Alert tone="ok">{t('notifications.chatFound', { value: found.join(', ') })}</Alert>}
      </form>
      {connection && <Deliveries connectionId={connection.id} />}
    </Modal>
  );
}

/** The events by group, a box per kind with its hint, and an "All" box per group. */
function EventPicker({ catalog, selected, onChange }: {
  catalog: ListConnectorTypesResponse;
  selected: string[];
  onChange: (events: string[]) => void;
}) {
  const { t } = useTranslation();
  return (
    <section className="notify-events">
      <h3>{t('notifications.eventsTitle')}</h3>
      {groupEvents(catalog.events).map(({ group, kinds }) => (
        <EventGroup key={group} group={group} kinds={kinds} selected={selected} onChange={onChange} />
      ))}
    </section>
  );
}

/** One group: its title with the "All" box, then a box per kind with its hint. */
function EventGroup({ group, kinds, selected, onChange }: {
  group: string;
  kinds: string[];
  selected: string[];
  onChange: (events: string[]) => void;
}) {
  const { t } = useTranslation();
  const titleId = useId();
  const chosen = kinds.filter((k) => selected.includes(k)).length;
  return (
    <div className="notify-group" role="group" aria-labelledby={titleId}>
      <div className="notify-group-head">
        <h4 id={titleId}>{t(`notifications.groups.${group}`, { defaultValue: group })}</h4>
        <label className="check-inline small">
          <AllBox label={t('notifications.allGroup', { group: t(`notifications.groups.${group}`, { defaultValue: group }) })} all={chosen === kinds.length} some={chosen > 0} onChange={(on) => onChange(toggleGroup(selected, kinds, on))} />
          {t('notifications.all')}
        </label>
      </div>
      {kinds.map((kind) => (
        <label key={kind} className="notify-event">
          <input type="checkbox" checked={selected.includes(kind)} onChange={(e) => onChange(toggleGroup(selected, [kind], e.target.checked))} />
          <span className="notify-event-text">
            <span>{t(`notifications.events.${kind}`, { defaultValue: kind })}</span>
            <span className="muted small">{t(`notifications.eventsHint.${kind}`, { defaultValue: '' })}</span>
          </span>
        </label>
      ))}
    </div>
  );
}

/** A group's "All" box: checked when every kind is, mixed when only some are. */
function AllBox({ label, all, some, onChange }: { label: string; all: boolean; some: boolean; onChange: (on: boolean) => void }) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = some && !all;
  }, [all, some]);
  return <input ref={ref} type="checkbox" aria-label={label} checked={all} onChange={(e) => onChange(e.target.checked)} />;
}
