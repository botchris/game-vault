import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, sourceClient } from '../../api/client';
import { Icon } from '../../components/Icon';
import { SourceLogo } from '../../components/PlatformBadge';
import { Alert } from '../../components/ui';
import type { Source, SourceType } from '../../gen/gamevault/v1/source_pb';
import { toDate } from '../../lib/model';
import { useAppData } from '../../state/AppData';
import SourceDialog from './SourceDialog';

/** Configured accounts (Humble Bundle, Steam...) that are scanned to keep the catalog up to date. */
export default function SourcesPage() {
  const { t } = useTranslation();
  const { sources, sourceTypes, reloadSources, reloadGames } = useAppData();
  const [editing, setEditing] = useState<{ type: SourceType; source?: Source } | null>(null);
  const [syncing, setSyncing] = useState<string | null>(null);
  const [showAllTypes, setShowAllTypes] = useState(false);
  const [error, setError] = useState('');

  const typeOf = (s: Source) => sourceTypes.find((x) => x.id === s.type);

  const sync = async (id: string | 'all') => {
    setSyncing(id);
    setError('');
    try {
      if (id === 'all') await sourceClient.syncAllSources({});
      else await sourceClient.syncSource({ id });
      await Promise.all([reloadSources(), reloadGames()]);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSyncing(null);
    }
  };

  // A second account of the same store is possible but rare: those types are behind "show all".
  const configured = new Set(sources.map((s) => s.type));
  const addable = showAllTypes ? sourceTypes : sourceTypes.filter((ty) => !configured.has(ty.id));
  const hiddenTypes = sourceTypes.length - addable.length;

  return (
    <div className="page sources">
      <header className="page-head">
        <h1 className="page-title">{t('sources.title')}</h1>
        {sources.length > 0 && (
          <button className="primary" onClick={() => sync('all')} disabled={!!syncing}>
            {syncing === 'all' ? t('sources.syncing') : t('sources.syncAll')}
          </button>
        )}
      </header>
      <p className="muted sources-intro">{t('sources.intro')}</p>

      {error && <Alert tone="error">{error}</Alert>}
      {syncing && <p className="muted small sources-hint">{t('sources.syncingHint')}</p>}

      {sources.length > 0 && (
        <ul className="source-list">
          {sources.map((s) => (
            <SourceRow key={s.id} source={s} typeName={typeOf(s)?.name ?? s.type}
              syncing={syncing === s.id || syncing === 'all'} busy={!!syncing}
              onSync={() => sync(s.id)}
              onEdit={() => { const ty = typeOf(s); if (ty) setEditing({ type: ty, source: s }); }} />
          ))}
        </ul>
      )}

      {addable.length === 0 && hiddenTypes > 0 && (
        <button className="link source-add-another" onClick={() => setShowAllTypes(true)}>{t('sources.addAnother')}</button>
      )}

      {addable.length > 0 && (
        <section className="source-add">
          <div className="source-add-head">
            <h2>{showAllTypes ? t('sources.addAnother') : sources.length > 0 ? t('sources.addMore') : t('sources.add')}</h2>
            {hiddenTypes > 0 && (
              <button className="link" onClick={() => setShowAllTypes(true)}>{t('sources.addAnother')}</button>
            )}
          </div>
          <ul className="source-tiles">
            {addable.map((ty) => (
              <li key={ty.id}>
                <button className="source-tile" onClick={() => setEditing({ type: ty })}>
                  <SourceLogo type={ty.id} size={36} />
                  <span className="source-tile-text">
                    <span className="source-tile-name">{ty.name}</span>
                    <span className="source-tile-tagline">{t(`sources.taglines.${ty.id}`, { defaultValue: '' })}</span>
                  </span>
                  <Icon name="plus" size={18} className="source-tile-plus" />
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}

      {editing && (
        <SourceDialog type={editing.type} source={editing.source} onClose={() => setEditing(null)}
          onSaved={async (id, syncNow) => {
            setEditing(null);
            await reloadSources();
            if (syncNow) await sync(id);
          }}
          onDeleted={async () => {
            setEditing(null);
            await Promise.all([reloadSources(), reloadGames()]);
          }} />
      )}
    </div>
  );
}

/** "Today, 16:10", "Yesterday, 09:02" or a date: scans are recent, so the day is what matters. */
function useWhen() {
  const { t, i18n } = useTranslation();
  return (d: Date) => {
    const time = d.toLocaleTimeString(i18n.language, { hour: 'numeric', minute: '2-digit' });
    const days = Math.round((startOfDay(new Date()) - startOfDay(d)) / 86_400_000);
    if (days === 0) return t('sources.today', { time });
    if (days === 1) return t('sources.yesterday', { time });
    return d.toLocaleDateString(i18n.language, { day: 'numeric', month: 'short', year: days > 300 ? 'numeric' : undefined });
  };
}

function startOfDay(d: Date) {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

function SourceRow(props: { source: Source; typeName: string; syncing: boolean; busy: boolean; onSync: () => void; onEdit: () => void }) {
  const { t } = useTranslation();
  const when = useWhen();
  const { source: s } = props;
  const r = s.lastSync;

  let tone = 'never';
  let status = t('sources.neverSynced');
  if (props.syncing) {
    tone = 'busy';
    status = t('sources.syncing');
  } else if (r && !r.success) {
    tone = 'error';
    status = r.error;
  } else if (r) {
    tone = r.warnings.length > 0 ? 'warn' : 'ok';
    const changes = r.copiesAdded > 0 ? t('sources.newCopies', { count: r.copiesAdded }) : t('sources.noChanges');
    status = [when(toDate(r.finishedAt)!), changes, r.excluded > 0 && t('sources.excludedCount', { count: r.excluded }),
      r.warnings.length > 0 && t('sources.warnings', { count: r.warnings.length })]
      .filter(Boolean).join(' · ');
  }

  const schedule = !s.enabled ? t('sources.paused') : s.syncIntervalHours ? t('sources.every', { count: s.syncIntervalHours }) : t('sources.manual');

  return (
    <li className={`source-row ${s.enabled ? '' : 'disabled'}`}>
      <SourceLogo type={s.type} />
      <button className="source-main" onClick={props.onEdit}>
        <span className="source-name">
          {s.name}
          {s.name !== props.typeName && <span className="source-type"> · {props.typeName}</span>}
        </span>
        <span className={`source-status tone-${tone}`}>
          <span className="source-dot" aria-hidden="true" />
          <span className="source-status-text">{status}</span>
        </span>
      </button>
      <span className="source-meta">
        <span className="source-count">{t('sources.copyCount', { count: s.copyCount })}</span>
        <span className="source-schedule">{schedule}</span>
      </span>
      <button className={`icon-button source-sync ${props.syncing ? 'spinning' : ''}`} onClick={props.onSync}
        disabled={props.busy} title={t('sources.syncNow')} aria-label={`${t('sources.syncNow')}: ${s.name}`}>
        <Icon name="refresh" size={18} />
      </button>
    </li>
  );
}
