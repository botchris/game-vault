import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { coverClient, errorMessage, providerClient } from '../../api/client';
import { Alert } from '../../components/ui';
import type { Provider } from '../../gen/gamevault/v1/provider_pb';
import { toDate } from '../../lib/model';
import { describeStatus } from '../../lib/providerStatus';
import { useAppData } from '../../state/AppData';
import ProviderDialog from './ProviderDialog';
import type { DialogIntent } from '../../lib/providerForm';
import { forgetPriceProviders } from '../../lib/usePriceProviders';
import { useHealth } from '../../state/Health';
import { useAction } from '../../lib/useAction';

type Notice = { tone: 'ok' | 'error'; text: string } | null;

/** Metadata provider chains, Plex-agent style: order, enable and configure each provider. */
export default function ProvidersPage() {
  const { t } = useTranslation();
  const { reloadGames } = useAppData();
  const [notice, setNotice] = useState<Notice>(null);
  const { busy, run } = useAction({ onError: (text) => setNotice({ tone: 'error', text }) });
  const { issues } = useHealth();
  const healthKey = issues.filter((i) => i.kind === 'provider').map((i) => `${i.key}:${i.reason}`).join(',');

  const refresh = async (missingOnly: boolean) => {
    if (!missingOnly && !confirm(t('providers.confirmRedownload'))) return;
    await run(async () => {
      const res = await coverClient.refreshCovers({ missingOnly });
      await reloadGames();
      setNotice({ tone: 'ok', text: t(missingOnly ? 'providers.retried' : 'providers.redownloaded', { count: res.games }) });
    });
  };

  return (
    <div className="page">
      <div>
        <h2>{t('providers.title')}</h2>
        <p className="muted">{t('providers.intro')}</p>
      </div>
      {notice && <Alert tone={notice.tone}>{notice.text}</Alert>}

      <ProviderChain kind="cover" healthKey={healthKey} title={t('providers.covers')} intro={t('providers.coversIntro')} onNotice={setNotice}
        onChanged={reloadGames} // covers may resolve differently now
        pinned={{ name: t('providers.custom.name'), description: t('providers.custom.description') }}
        actions={<>
          <button onClick={() => refresh(true)} disabled={busy}>{t('providers.retryMissing')}</button>
          <button onClick={() => refresh(false)} disabled={busy}>{t('providers.redownload')}</button>
        </>} />

      <ProviderChain kind="barcode" healthKey={healthKey} title={t('providers.barcodes')} intro={t('providers.barcodesIntro')} onNotice={setNotice}
        pinned={{ name: t('providers.local.name'), description: t('providers.local.description') }} />

      <ProviderChain kind="metadata" healthKey={healthKey} title={t('providers.metadata')} intro={t('providers.metadataIntro')} onNotice={setNotice} />

      <ProviderChain kind="valuation" healthKey={healthKey} title={t('providers.valuations')} intro={t('providers.valuationsIntro')} onNotice={setNotice}
        onChanged={async () => forgetPriceProviders()} />
    </div>
  );
}

function ProviderChain(props: {
  kind: 'cover' | 'barcode' | 'metadata' | 'valuation';
  title: string;
  intro: string;
  pinned?: { name: string; description: string };
  actions?: React.ReactNode;
  onNotice: (n: Notice) => void;
  onChanged?: () => Promise<void>;
  /** Changes when the providers' health issues change. */
  healthKey: string;
}) {
  const { t, i18n } = useTranslation();
  const { kind, onNotice, onChanged } = props;
  const { reload: reloadHealth } = useHealth();
  const [providers, setProviders] = useState<Provider[]>([]);
  const [editing, setEditing] = useState<Provider | null>(null);
  const [intent, setIntent] = useState<DialogIntent>('configure');
  // A failure shows in the page's notice, with the other chains'.
  const action = useAction({ onError: (text) => onNotice({ tone: 'error', text }) });
  const busy = action.busy;

  const load = useCallback(async () => {
    try {
      const res = await providerClient.listProviders({ kind });
      setProviders(res.providers);
    } catch (e) {
      onNotice({ tone: 'error', text: errorMessage(e) });
    }
  }, [kind, onNotice]);

  // The chain is read again when the providers' health changes (an allowance renewed, a key fixed),
  // so its badges follow without polling every chain.
  useEffect(() => {
    load();
  }, [load, props.healthKey]);

  const run = (fn: () => Promise<void>) => {
    onNotice(null);
    return action.run(async () => {
      await fn();
      reloadHealth(); // new settings may fix a provider's problem
      await onChanged?.();
    });
  };

  const move = (index: number, delta: number) => run(async () => {
    const ids = providers.map((p) => p.id);
    const [id] = ids.splice(index, 1);
    ids.splice(index + delta, 0, id!);
    setProviders((await providerClient.reorderProviders({ kind, ids })).providers);
  });

  const toggle = (p: Provider) => {
    const missingRequired = p.fields.some((f) => f.required && !p.settings[f.key]);
    if (!p.enabled && missingRequired) {
      setIntent('enable');
      setEditing(p); // needs credentials first
      return;
    }
    run(async () => {
      await providerClient.updateProvider({ id: p.id, enabled: !p.enabled, settings: p.settings });
      await load();
    });
  };

  return (
    <section className="card">
      <div className="section-head">
        <div>
          <h3>{props.title}</h3>
          <p className="muted small">{props.intro}</p>
        </div>
        {props.actions && <div className="actions">{props.actions}</div>}
      </div>
      <ol className="chain">
        {props.pinned && (
          <li className="chain-item pinned">
            <span className="chain-rank">★</span>
            <div className="chain-body">
              <strong>{props.pinned.name}</strong>
              <span className="muted small">{props.pinned.description}</span>
            </div>
            <span className="badge">{t('providers.alwaysFirst')}</span>
          </li>
        )}
        {providers.map((p, i) => (
          <li key={p.id} className={`chain-item ${p.enabled ? '' : 'disabled'}`}>
            <span className="chain-rank">{i + 1}</span>
            <div className="chain-body">
              <strong>{p.name}</strong>
              <span className="muted small">{t(p.descriptionKey)}</span>
              {(() => {
                const st = p.status;
                return st && (
                  <>
                    <span className="badge warn provider-status">
                      {describeStatus(st, toDate(st.until) ?? undefined, new Date(), t, i18n.language)}
                    </span>
                    <span className="muted small provider-status-message">{st.message}</span>
                  </>
                );
              })()}
            </div>
            <div className="chain-actions">
              <button className="icon" title={t('providers.moveUp')} disabled={busy || i === 0} onClick={() => move(i, -1)}>▲</button>
              <button className="icon" title={t('providers.moveDown')} disabled={busy || i === providers.length - 1} onClick={() => move(i, 1)}>▼</button>
              <label className="switch" title={p.enabled ? t('providers.disable') : t('providers.enable')}>
                <input type="checkbox" checked={p.enabled} disabled={busy} onChange={() => toggle(p)} />
                <span>{p.enabled ? t('providers.on') : t('providers.off')}</span>
              </label>
              {p.fields.length > 0 && <button onClick={() => { setIntent('configure'); setEditing(p); }}>{t('providers.configure')}</button>}
            </div>
          </li>
        ))}
      </ol>
      {editing && (
        <ProviderDialog provider={editing} intent={intent} onClose={() => setEditing(null)}
          onSaved={() => { setEditing(null); run(load); }} />
      )}
    </section>
  );
}
