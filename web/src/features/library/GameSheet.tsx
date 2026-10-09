import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, mediaUrl, metadataClient } from '../../api/client';
import { Icon } from '../../components/Icon';
import Lightbox from '../../components/Lightbox';
import { Alert, useFormatters } from '../../components/ui';
import VideoPlayer from '../../components/VideoPlayer';
import type { GameDetails } from '../../gen/gamevault/v1/metadata_pb';
import { toDate, type Game } from '../../lib/model';

/** Loads the game's details from the metadata providers (cached server-side). */
export function useGameDetails(game: Game) {
  const { i18n } = useTranslation();
  const [details, setDetails] = useState<GameDetails | null>(null);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const linksKey = JSON.stringify(Object.entries(game.links).sort());

  const load = useCallback(async (refresh = false) => {
    setLoading(true);
    setError('');
    try {
      const res = await metadataClient.getGameDetails({ gameId: game.id, language: i18n.resolvedLanguage ?? 'en', refresh });
      setDetails(res.details ?? null);
      setWarnings(res.warnings);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
    // Reload when the links or cover change (the server invalidates its cache then too).
  }, [game.id, linksKey, game.coverUrl, i18n.resolvedLanguage]);

  useEffect(() => {
    load();
  }, [load]);

  return { details, warnings, loading, error, refresh: () => load(true) };
}

/** Header facts next to the cover: release, genres, companies, rating, players, Metacritic. */
export function SheetFacts({ details }: { details: GameDetails | null }) {
  const { t } = useTranslation();
  const fmt = useFormatters();
  if (!details) return null;
  const release = /^\d{4}-\d{2}-\d{2}$/.test(details.releaseDate) ? fmt.date(details.releaseDate) : details.releaseDate;
  const facts: [string, string][] = [
    [t('details.release'), release],
    [t('details.developer'), details.developers.join(', ')],
    [t('details.publisher'), details.publishers.join(', ')],
    [t('details.ageRating'), details.ageRating],
    [t('details.players'), details.players],
  ];
  return (
    <div className="facts">
      <dl className="fact-grid">
        {facts.filter(([, v]) => v).map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
      </dl>
      <div className="links">
        {details.metacritic > 0 && (
          <a className={`metacritic ${details.metacritic >= 75 ? 'good' : details.metacritic >= 50 ? 'mixed' : 'bad'}`}
            href={details.metacriticUrl || undefined} target="_blank" rel="noreferrer" title="Metacritic"><b>{details.metacritic}</b>Metacritic</a>
        )}
        {details.storeUrl && <a className="button small-button" href={details.storeUrl} target="_blank" rel="noreferrer">{t('details.storePage')}<Icon name="external" size={14} /></a>}
        {details.website && <a className="button small-button" href={details.website} target="_blank" rel="noreferrer">{t('details.website')}<Icon name="external" size={14} /></a>}
      </div>
    </div>
  );
}

/** The "Overview" tab: summary, trailers and screenshots. */
export function SheetOverview({ game, details, warnings, loading, error, onRefresh }: {
  game: Game;
  details: GameDetails | null;
  warnings: string[];
  loading: boolean;
  error: string;
  onRefresh: () => void;
}) {
  const { t } = useTranslation();
  const fmt = useFormatters();
  const [shot, setShot] = useState<number | null>(null);
  const empty = details && !details.summary && details.videos.length === 0 && details.screenshots.length === 0;

  return (
    <div className="sheet">
      {error && <Alert tone="error">{error}</Alert>}
      {warnings.map((w) => <Alert key={w} tone="warn">{w}</Alert>)}
      {loading && !details && <p className="muted">{t('details.loading')}</p>}
      {empty && <p className="muted">{t(Object.keys(game.links).length > 0 ? 'details.empty' : 'details.emptyNoLinks')}</p>}

      {details?.summary && (
        <section>
          <h3>{t('details.summary')}</h3>
          <Summary text={details.summary} />
        </section>
      )}
      {game.notes && (
        <section>
          <h3>{t('common.notes')}</h3>
          <div className="summary"><p>{game.notes}</p></div>
        </section>
      )}
      {details && details.videos.length > 0 && (
        <section>
          <h3>{t('details.trailers')}</h3>
          <VideoPlayer videos={details.videos} />
        </section>
      )}
      {details && details.screenshots.length > 0 && (
        <section>
          <h3>{t('details.screenshots')}</h3>
          <div className="shots">
            {details.screenshots.map((s, i) => (
              <button key={s.fullUrl} onClick={() => setShot(i)}><img src={mediaUrl(s.thumbUrl || s.fullUrl)} alt="" loading="lazy" /></button>
            ))}
          </div>
        </section>
      )}
      {shot !== null && details && (
        <Lightbox images={details.screenshots.map((s) => s.fullUrl)} index={shot} onIndex={setShot} onClose={() => setShot(null)} />
      )}

      {details && (
        <p className="muted small sheet-footer">
          {details.sources.length > 0 && t('details.sources', { sources: details.sources.join(' + ') })}
          {details.fetchedAt && ` · ${t('details.updated', { date: fmt.dateTime(toDate(details.fetchedAt)) })}`}
          {' · '}<button className="link" onClick={onRefresh} disabled={loading}>{loading ? t('common.loading') : t('details.refresh')}</button>
        </p>
      )}
    </div>
  );
}

/** Long synopses are clamped to a few lines with a "Read more" toggle. */
function Summary({ text }: { text: string }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const long = text.length > 600;
  return (
    <>
      <div className={`summary ${long && !open ? 'clamped' : ''}`}>{text.split(/\n{2,}/).map((p, i) => <p key={i}>{p}</p>)}</div>
      {long && <button className="link" onClick={() => setOpen(!open)} aria-expanded={open}>{open ? t('details.readLess') : t('details.readMore')}</button>}
    </>
  );
}
