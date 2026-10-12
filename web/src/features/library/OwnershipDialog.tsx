import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, ownershipClient } from '../../api/client';
import { PlatformBadge } from '../../components/PlatformBadge';
import { Alert, Modal } from '../../components/ui';
import { CopyKind } from '../../lib/model';
import { chunk, cleanTitles, notOwnedText, summarize, toRows, type GameLike, type ResultLike, type Row } from '../../lib/ownership';

const KINDS: Record<string, CopyKind> = { key: CopyKind.KEY, library: CopyKind.LIBRARY, physical: CopyKind.PHYSICAL };
const MARKS: Record<Row['status'], string> = { owned: '✓', possible: '?', not_owned: '–' };

/** "Do I own it?": checks a pasted list of titles (a bundle, a sale) against the catalog. Nothing is
 *  saved. On phones it is full screen, with the list above the results. */
export default function OwnershipDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const [text, setText] = useState('');
  const [rows, setRows] = useState<Row[] | null>(null);
  const [skipped, setSkipped] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);

  const check = async (e: FormEvent) => {
    e.preventDefault();
    const cleaned = cleanTitles(text);
    setSkipped(cleaned.skipped);
    setError('');
    setCopied(false);
    if (!cleaned.titles.length) {
      setRows(null);
      setError(t('ownership.nothing'));
      return;
    }
    setBusy(true);
    try {
      const results: ResultLike[] = [];
      for (const part of chunk(cleaned.titles)) {
        results.push(...(await ownershipClient.checkOwnership({ items: part.map((title) => ({ title })) })).results);
      }
      setRows(toRows(cleaned.titles, results));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const copyMissing = async () => {
    if (!rows) return;
    try {
      await navigator.clipboard.writeText(notOwnedText(rows));
      setCopied(true);
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const sum = rows ? summarize(rows) : null;
  return (
    <Modal title={t('ownership.title')} onClose={onClose} wide phoneFull
      footer={<>
        <span className="spacer" />
        <button type="button" onClick={onClose}>{t('common.close')}</button>
        <button type="submit" form="ownership-form" className="primary" disabled={busy || !text.trim()}>
          {busy ? t('ownership.checking') : t('ownership.check')}
        </button>
      </>}>
      <div className="ownership">
        <form id="ownership-form" className="ownership-input" onSubmit={check}>
          <label>
            <span className="muted small">{t('ownership.intro')}</span>
            <textarea value={text} onChange={(e) => setText(e.target.value)} placeholder={t('ownership.placeholder')}
              spellCheck={false} autoFocus />
          </label>
        </form>
        <section className="ownership-results" aria-live="polite" aria-label={t('ownership.results')}>
          {error && <Alert tone="error">{error}</Alert>}
          {skipped > 0 && <p className="muted small">{t('ownership.skipped', { count: skipped })}</p>}
          {rows && sum && (
            <>
              <div className="ownership-summary">
                <span>{t('ownership.summary', { owned: sum.owned, total: sum.total })}</span>
                {sum.possible > 0 && <span className="muted">{t('ownership.possibleCount', { count: sum.possible })}</span>}
                <span className="spacer" />
                {sum.owned + sum.possible < sum.total && (
                  <button type="button" onClick={copyMissing}>{copied ? t('ownership.copied') : t('ownership.copyMissing')}</button>
                )}
              </div>
              <ul className="ownership-rows">
                {rows.map((r, i) => <ResultRow key={i} row={r} onOpen={onClose} />)}
              </ul>
            </>
          )}
        </section>
      </div>
    </Modal>
  );
}

function ResultRow({ row, onOpen }: { row: Row; onOpen: () => void }) {
  const { t } = useTranslation();
  const label = t(`ownership.status.${row.status}`);
  return (
    <li className={row.status}>
      <span className="state" aria-hidden="true">{MARKS[row.status]}</span>
      <span className="ownership-title">{row.title}<span className="sr-only"> · {label}</span></span>
      {row.status === 'owned' && row.game && (
        <span className="ownership-detail">
          <GameLink game={row.game} onOpen={onOpen} />
          {row.game.copies.map((c, i) => (
            <PlatformBadge key={i} platform={c.platform} holding={c.pendingKey ? 'key' : 'owned'} kind={KINDS[c.kind]} full />
          ))}
          {row.game.copies.some((c) => c.pendingKey) && <span>{t('ownership.pendingKey')}</span>}
        </span>
      )}
      {row.status === 'possible' && (
        <span className="ownership-detail">
          {t('ownership.maybe')}
          {row.candidates.map((g) => <GameLink key={g.id} game={g} onOpen={onOpen} />)}
        </span>
      )}
      {row.status === 'not_owned' && <span className="ownership-detail">{label}</span>}
    </li>
  );
}

/** A link to a game's sheet ("#/game/<id>"): the library opens it once the dialog closes. */
function GameLink({ game, onOpen }: { game: GameLike; onOpen: () => void }) {
  const href = game.path.startsWith('#/game/') ? game.path : '#/library';
  return <a href={href} onClick={onOpen}>{game.title}</a>;
}
