import { useEffect, useRef, useState, type FormEvent, type MouseEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, ownershipClient } from '../../api/client';
import { PlatformBadge } from '../../components/PlatformBadge';
import { Alert, Modal } from '../../components/ui';
import { copyText } from '../../lib/clipboard';
import { CopyKind } from '../../lib/model';
import { ShortAnswerError, checkAll, cleanTitles, notOwnedText, summarize, type GameLike, type Row } from '../../lib/ownership';

const KINDS: Record<string, CopyKind> = { key: CopyKind.KEY, library: CopyKind.LIBRARY, physical: CopyKind.PHYSICAL };
const MARKS: Record<Row['status'], string> = { owned: '✓', possible: '?', not_owned: '–' };

/** "Do I own it?": checks a pasted list of titles (a bundle, a sale) against the catalog. Nothing is
 *  saved. On phones it is full screen, with the list above the results. */
export default function OwnershipDialog({ onClose, onOpenGame }: { onClose: () => void; onOpenGame: (id: string) => void }) {
  const { t } = useTranslation();
  const [text, setText] = useState('');
  const [rows, setRows] = useState<Row[] | null>(null);
  const [skipped, setSkipped] = useState(0);
  const [tooLong, setTooLong] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);
  // The text to copy by hand when the browser refuses to copy.
  const [manual, setManual] = useState('');
  const manualField = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    if (!manual) return;
    manualField.current?.focus();
    manualField.current?.select();
  }, [manual]);

  const check = async (e: FormEvent) => {
    e.preventDefault();
    const cleaned = cleanTitles(text);
    setSkipped(cleaned.skipped);
    setTooLong(cleaned.tooLong);
    setManual('');
    setError('');
    setCopied(false);
    if (!cleaned.titles.length) {
      setRows(null);
      setError(t('ownership.nothing'));
      return;
    }
    setBusy(true);
    const done = await checkAll(cleaned.titles, async (part) => (await ownershipClient.checkOwnership({ items: part.map((title) => ({ title })) })).results);
    setRows(done.rows.length ? done.rows : null);
    if (done.error) setError(done.error instanceof ShortAnswerError ? t('ownership.shortAnswer') : errorMessage(done.error));
    setBusy(false);
  };

  const copyMissing = async () => {
    if (!rows) return;
    const text = notOwnedText(rows);
    const ok = await copyText(text);
    setCopied(ok);
    setManual(ok ? '' : text);
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
              spellCheck={false} autoFocus={!window.matchMedia('(max-width: 860px)').matches} />
          </label>
        </form>
        <section className="ownership-results" aria-label={t('ownership.results')}>
          <div role="status" aria-live="polite">
            {error && <Alert tone="error">{error}</Alert>}
            {sum && <p className="ownership-summary-line">{t('ownership.summary', { owned: sum.owned, total: sum.total })}</p>}
          </div>
          {skipped > 0 && <p className="muted small">{t('ownership.skipped', { count: skipped })}</p>}
          {tooLong > 0 && <p className="muted small">{t('ownership.tooLong', { count: tooLong })}</p>}
          {rows && sum && (
            <>
              <div className="ownership-summary">
                {sum.possible > 0 && <span className="muted">{t('ownership.possibleCount', { count: sum.possible })}</span>}
                <span className="spacer" />
                {sum.owned + sum.possible < sum.total && (
                  <button type="button" onClick={copyMissing}>{copied ? t('ownership.copied') : t('ownership.copyMissing')}</button>
                )}
              </div>
              {manual && (
                <label className="ownership-manual">
                  <span className="muted small">{t('ownership.copyManually')}</span>
                  <textarea ref={manualField} readOnly value={manual} rows={Math.min(8, manual.split('\n').length)} spellCheck={false}
                    onFocus={(e) => e.currentTarget.select()} />
                </label>
              )}
              <ul className="ownership-rows">
                {rows.map((r, i) => <ResultRow key={i} row={r} onOpen={onOpenGame} />)}
              </ul>
            </>
          )}
        </section>
      </div>
    </Modal>
  );
}

function ResultRow({ row, onOpen }: { row: Row; onOpen: (id: string) => void }) {
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

/** A link to a game's sheet ("#/game/<id>"). A plain click opens the sheet through the library, so it
 *  opens even when the address does not change; a modified or middle click keeps the browser's own
 *  behaviour (new tab, new window) and leaves the dialog open. */
function GameLink({ game, onOpen }: { game: GameLike; onOpen: (id: string) => void }) {
  const href = game.path.startsWith('#/game/') ? game.path : '#/library';
  const click = (e: MouseEvent<HTMLAnchorElement>) => {
    if (e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    onOpen(game.id);
  };
  return <a href={href} onClick={click}>{game.title}</a>;
}
