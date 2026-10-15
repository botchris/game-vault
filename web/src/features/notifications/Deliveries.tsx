import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { notificationClient } from '../../api/client';
import { Alert } from '../../components/ui';
import type { Delivery } from '../../gen/gamevault/v1/notify_pb';
import { sinceText } from '../../lib/health';
import { toDate } from '../../lib/model';
import { useAction } from '../../lib/useAction';

/** A connection's latest deliveries (the server keeps the last 20), newest first: state, title,
 *  when, and why the last attempt failed. */
export default function Deliveries({ connectionId }: { connectionId: string }) {
  const { t, i18n } = useTranslation();
  const [deliveries, setDeliveries] = useState<Delivery[] | null>(null);
  const { error, run } = useAction();

  useEffect(() => {
    void run(async () => {
      const res = await notificationClient.listDeliveries({ connectionId });
      setDeliveries(res.deliveries);
    });
  }, [run, connectionId]);

  const now = new Date();
  return (
    <section className="notify-deliveries">
      <h3>{t('notifications.deliveries')}</h3>
      {error && <Alert tone="error">{error}</Alert>}
      {deliveries?.length === 0 && <p className="muted small">{t('notifications.noDeliveries')}</p>}
      {deliveries && deliveries.length > 0 && (
        <ul>
          {deliveries.map((d) => {
            const when = toDate(d.updatedAt) ?? toDate(d.createdAt);
            return (
              <li key={d.id} className="notify-delivery">
                <div className="notify-delivery-head">
                  <span className={`badge notify-state ${d.state}`}>{t(`notifications.delivery.${d.state}`, { defaultValue: d.state })}</span>
                  <span className="notify-delivery-summary">{d.summary || t(`notifications.events.${d.event}`, { defaultValue: d.event })}</span>
                  {when && <time className="muted small" dateTime={when.toISOString()}>{sinceText(when, now, i18n.language)}</time>}
                </div>
                {d.lastError && (
                  <p className="small danger notify-delivery-error">
                    {d.attempts > 0 && `${t('notifications.attempts', { count: d.attempts })}: `}{d.lastError}
                  </p>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
