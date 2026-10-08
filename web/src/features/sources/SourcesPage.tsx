import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, sourceClient } from '../../api/client';
import { Alert, useFormatters } from '../../components/ui';
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

  const remove = async (s: Source) => {
    if (!confirm(t('sources.confirmDelete', { name: s.name }))) return;
    const deleteCopies = s.copyCount > 0 && confirm(t('sources.confirmDeleteCopies', { count: s.copyCount }));
    try {
      await sourceClient.deleteSource({ id: s.id, deleteCopies });
      await Promise.all([reloadSources(), reloadGames()]);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  return (
    <div className="page">
      <div className="section-head">
        <div>
          <h2>{t('sources.title')}</h2>
          <p className="muted">{t('sources.intro')}</p>
        </div>
        <button onClick={() => sync('all')} disabled={!!syncing || sources.length === 0}>
          {syncing === 'all' ? t('sources.syncing') : t('sources.syncAll')}
        </button>
      </div>

      {error && <Alert tone="error">{error}</Alert>}

      <div className="cards">
        {sources.map((s) => (
          <SourceCard key={s.id} source={s} typeName={typeOf(s)?.name ?? s.type} syncing={syncing === s.id || syncing === 'all'}
            onSync={() => sync(s.id)} onEdit={() => { const ty = typeOf(s); if (ty) setEditing({ type: ty, source: s }); }}
            onDelete={() => remove(s)} />
        ))}
      </div>

      <h3>{t('sources.add')}</h3>
      <div className="cards">
        {sourceTypes.map((ty) => (
          <button key={ty.id} className="card add-card" onClick={() => setEditing({ type: ty })}>
            <strong>+ {ty.name}</strong>
            <span className="muted small">{t(ty.descriptionKey)}</span>
          </button>
        ))}
      </div>

      {editing && (
        <SourceDialog type={editing.type} source={editing.source} onClose={() => setEditing(null)}
          onSaved={async (id, syncNow) => {
            setEditing(null);
            await reloadSources();
            if (syncNow) await sync(id);
          }} />
      )}
    </div>
  );
}

function SourceCard(props: { source: Source; typeName: string; syncing: boolean; onSync: () => void; onEdit: () => void; onDelete: () => void }) {
  const { t } = useTranslation();
  const fmt = useFormatters();
  const { source: s } = props;
  const r = s.lastSync;
  return (
    <section className={`card source ${s.enabled ? '' : 'disabled'}`}>
      <header className="source-head">
        <div>
          <h3>{s.name}</h3>
          <span className="muted small">
            {props.typeName} · {s.syncIntervalHours ? t('sources.every', { count: s.syncIntervalHours }) : t('sources.manual')}
            {!s.enabled && ` · ${t('sources.disabled')}`}
          </span>
        </div>
        <span className="badge">{t('sources.copyCount', { count: s.copyCount })}</span>
      </header>
      {!r ? (
        <p className="muted small">{t('sources.neverSynced')}</p>
      ) : r.success ? (
        <p className="small">
          <span className="ok">✓</span> {fmt.dateTime(toDate(r.finishedAt))} — {t('sources.report', {
            fetched: r.fetched, added: r.copiesAdded, updated: r.copiesUpdated, games: r.gamesCreated,
          })}
          {r.warnings.length > 0 && <details><summary>{t('sources.warnings', { count: r.warnings.length })}</summary><ul>{r.warnings.map((w, i) => <li key={i}>{w}</li>)}</ul></details>}
        </p>
      ) : (
        <Alert tone="error">{fmt.dateTime(toDate(r.finishedAt))} — {r.error}</Alert>
      )}
      {props.syncing && <p className="muted small">{t('sources.syncingHint')}</p>}
      <div className="actions">
        <button className="primary" onClick={props.onSync} disabled={props.syncing}>{props.syncing ? t('sources.syncing') : t('sources.syncNow')}</button>
        <button onClick={props.onEdit}>{t('common.edit')}</button>
        <span className="spacer" />
        <button className="danger" onClick={props.onDelete}>{t('common.delete')}</button>
      </div>
    </section>
  );
}
