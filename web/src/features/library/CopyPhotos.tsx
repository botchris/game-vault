import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { timestampFromDate } from '@bufbuild/protobuf/wkt';
import { errorMessage, gameClient, photoUrl } from '../../api/client';
import { Icon } from '../../components/Icon';
import Lightbox from '../../components/Lightbox';
import { preparePhoto, UnreadablePhotoError, uploadPhoto } from '../../lib/photos';
import { toDate, type Copy, type Game } from '../../lib/model';
import { useAppData } from '../../state/AppData';

const STRIP = 6;
const MAX_PHOTOS = 50;

interface Upload { key: string; name: string; progress: number; error?: string }

/** A copy's photos: a strip of thumbnails, "Add photos" with per-file progress, and the viewer.
 *  `onEditionCover` is told the system whose cover a photo became (the copy's edition), so the sheet
 *  can show it. */
export default function CopyPhotos({ game, copy, onEditionCover }: {
  game: Game;
  copy: Copy;
  onEditionCover?: (system: string) => void;
}) {
  const { t } = useTranslation();
  const { putGame } = useAppData();
  const [uploads, setUploads] = useState<Upload[]>([]);
  const [open, setOpen] = useState<number | null>(null);
  const photos = copy.photos;

  const update = (key: string, patch: Partial<Upload>) =>
    setUploads((list) => list.map((u) => (u.key === key ? { ...u, ...patch } : u)));

  // One file at a time: each is reduced in memory first, and attaching one by one shows each photo
  // as soon as it is ready.
  const add = async (files: File[]) => {
    // Uploads still in flight will take room too.
    const pending = uploads.filter((u) => !u.error).length;
    const room = MAX_PHOTOS - photos.length - pending;
    const taken = files.slice(0, Math.max(0, room));
    const skipped = files.length - taken.length;
    const batch = taken.map((f, i) => ({ key: `${Date.now()}-${i}`, name: f.name, progress: 0 }));
    setUploads((list) => [
      ...list,
      ...batch,
      ...(skipped > 0 ? [{ key: `${Date.now()}-limit`, name: '', progress: 0, error: t('photos.tooMany', { max: MAX_PHOTOS, count: skipped }) }] : []),
    ]);
    for (const [i, file] of taken.entries()) {
      const { key } = batch[i]!;
      try {
        const prepared = await preparePhoto(file);
        const up = await uploadPhoto(prepared, (p) => update(key, { progress: p }));
        const res = await gameClient.addCopyPhotos({
          gameId: game.id,
          copyId: copy.id,
          photos: [{ id: up.id, takenAt: up.takenAt ? timestampFromDate(up.takenAt) : undefined }],
        });
        putGame(res.game!);
        setUploads((list) => list.filter((u) => u.key !== key));
      } catch (e) {
        update(key, { error: e instanceof UnreadablePhotoError ? t('photos.unreadable') : errorMessage(e) });
      }
    }
  };

  return (
    <div className="copy-photos">
      {photos.length > 0 && (
        <ul className="photo-strip">
          {photos.slice(0, STRIP).map((p, i) => (
            <li key={p.id}>
              <button onClick={() => setOpen(i)} aria-label={p.caption || t('photos.open', { n: i + 1 })}>
                <img src={photoUrl(p.id, true)} alt="" loading="lazy" />
                {editionCoverPhoto(game, copy) === p.id && <span className="photo-cover" title={t('photos.isCover')}><Icon name="star" size={12} /></span>}
              </button>
            </li>
          ))}
          {photos.length > STRIP && (
            <li><button className="photo-more" onClick={() => setOpen(STRIP)}>{t('photos.more', { count: photos.length - STRIP })}</button></li>
          )}
        </ul>
      )}
      {uploads.length > 0 && (
        <ul className="photo-uploads">
          {uploads.map((u) => (
            <li key={u.key} className={u.error ? 'failed' : ''}>
              {u.error ? (
                <>
                  <span className="small">{u.name ? t('photos.failed', { name: u.name, error: u.error }) : u.error}</span>
                  <button className="link" onClick={() => setUploads((l) => l.filter((x) => x.key !== u.key))}>{t('photos.dismiss')}</button>
                </>
              ) : (
                <>
                  <span className="small muted">{u.name}</span>
                  <progress max={1} value={u.progress} aria-label={t('photos.uploading', { name: u.name })} />
                </>
              )}
            </li>
          ))}
        </ul>
      )}
      <label className="button small-button file">
        <Icon name="photo" size={16} />{t('photos.add')}
        <input type="file" accept="image/*" multiple hidden
          onChange={(e) => { const files = [...(e.target.files ?? [])]; e.target.value = ''; if (files.length) void add(files); }} />
      </label>
      {open !== null && photos[open] && (
        <Lightbox images={photos.map((p) => photoUrl(p.id))} index={open} onIndex={setOpen} onClose={() => setOpen(null)}
          label={t('photos.title')} footer={<PhotoFooter game={game} copy={copy} index={open} onIndex={setOpen} onEmpty={() => setOpen(null)} onEditionCover={onEditionCover} />} />
      )}
    </div>
  );
}

/** Under the photo in the viewer: caption (edited in place), date taken, order, cover, delete. */
function PhotoFooter({ game, copy, index, onIndex, onEmpty, onEditionCover }: {
  game: Game;
  copy: Copy;
  index: number;
  onIndex: (i: number) => void;
  onEmpty: () => void;
  onEditionCover?: (system: string) => void;
}) {
  const { t, i18n } = useTranslation();
  const { putGame } = useAppData();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const photos = copy.photos;
  const photo = photos[index]!;
  const [caption, setCaption] = useState(photo.caption);
  const [editingFor, setEditingFor] = useState(photo.id);
  if (editingFor !== photo.id) { setEditingFor(photo.id); setCaption(photo.caption); }
  // A photo is the cover of its copy's edition; that edition may not be the one the game shows.
  const system = copy.effectiveSystem;
  const isCover = editionCoverPhoto(game, copy) === photo.id;
  const isMain = game.editions.find((e) => e.system === system)?.main ?? true;
  const taken = toDate(photo.takenAt);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError('');
    try { await fn(); } catch (e) { setError(errorMessage(e)); } finally { setBusy(false); }
  };
  const ids = photos.map((p) => p.id);
  const move = (delta: number) => run(async () => {
    const order = [...ids];
    [order[index], order[index + delta]] = [order[index + delta]!, order[index]!];
    putGame((await gameClient.reorderCopyPhotos({ gameId: game.id, copyId: copy.id, photoIds: order })).game!);
    onIndex(index + delta);
  });
  // Escape cancels the edit: the blur that follows must not save what was typed.
  const cancelled = useRef(false);
  const saveCaption = () => {
    if (cancelled.current) { cancelled.current = false; return; }
    if (caption.trim() === photo.caption) return;
    void run(async () => putGame((await gameClient.updateCopyPhoto({ gameId: game.id, copyId: copy.id, photoId: photo.id, caption })).game!));
  };

  return (
    <div className="photo-footer">
      <input className="photo-caption" value={caption} maxLength={200} placeholder={t('photos.captionPlaceholder')} aria-label={t('photos.caption')}
        onChange={(e) => setCaption(e.target.value)} onBlur={saveCaption}
        onKeyDown={(e) => { if (e.key === 'Enter') e.currentTarget.blur(); if (e.key === 'Escape') { cancelled.current = true; setCaption(photo.caption); e.currentTarget.blur(); } }} />
      <div className="photo-actions">
        {taken && <span className="small muted">{t('photos.takenOn', { date: taken.toLocaleDateString(i18n.language, { timeZone: 'UTC', dateStyle: 'medium' }) })}</span>}
        <span className="spacer" />
        <button className="lightbox-button" disabled={busy || index === 0} onClick={() => move(-1)} aria-label={t('photos.moveLeft')} title={t('photos.moveLeft')}><Icon name="arrowLeft" size={18} /></button>
        <button className="lightbox-button" disabled={busy || index === photos.length - 1} onClick={() => move(1)} aria-label={t('photos.moveRight')} title={t('photos.moveRight')}><Icon name="arrowRight" size={18} /></button>
        <button className={`lightbox-button ${isCover ? 'active' : ''}`} disabled={busy} aria-pressed={isCover}
          title={system} onClick={() => run(async () => {
            putGame((await gameClient.setEditionCover({
              gameId: game.id, system,
              cover: isCover ? { case: 'clear', value: true } : { case: 'photoId', value: photo.id },
            })).game!);
            if (!isCover) onEditionCover?.(system);
          })}>
          <Icon name="star" size={18} />{t(isCover ? 'photos.stopCover' : 'photos.useAsCover')}
        </button>
        {/* Outside the main edition, the photo can also become the cover the library shows "By game". */}
        {!isMain && (
          <button className="lightbox-button" disabled={busy} onClick={() => run(async () => {
            if (!isCover) putGame((await gameClient.setEditionCover({ gameId: game.id, system, cover: { case: 'photoId', value: photo.id } })).game!);
            putGame((await gameClient.setMainEdition({ gameId: game.id, system })).game!);
            onEditionCover?.(system);
          })}>
            {t('edition.useAsMain')}
          </button>
        )}
        <button className="lightbox-button" disabled={busy} aria-label={t('common.delete')} title={t('common.delete')}
          onClick={() => {
            if (!confirm(t('photos.confirmDelete'))) return;
            void run(async () => {
              putGame((await gameClient.removeCopyPhoto({ gameId: game.id, copyId: copy.id, photoId: photo.id })).game!);
              if (photos.length === 1) onEmpty();
              else if (index === photos.length - 1) onIndex(index - 1);
            });
          }}><Icon name="trash" size={18} /></button>
      </div>
      {error && <p className="small photo-error">{error}</p>}
    </div>
  );
}

/** The photo chosen as the cover of the copy's edition; '' when none. */
function editionCoverPhoto(game: Game, copy: Copy): string {
  return game.editions.find((e) => e.system === copy.effectiveSystem)?.coverPhotoId ?? '';
}
