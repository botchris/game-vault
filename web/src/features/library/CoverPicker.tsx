import { useEffect, useRef, useState, type DragEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { coverClient, errorMessage, photoUrl, proxiedImage } from '../../api/client';
import { WholeCover } from '../../components/Cover';
import { Icon } from '../../components/Icon';
import ProvidersUnavailable from '../../components/ProvidersUnavailable';
import { Alert, Modal } from '../../components/ui';
import type { Game } from '../../gen/gamevault/v1/game_pb';
import type { CoverCandidate } from '../../gen/gamevault/v1/provider_pb';
import { cachedRequest } from '../../lib/requestCache';
import { googleImagesUrl, imageFromTransfer, isHttpUrl, isSmallPreview, steamGridDbUrl, type DroppedImage } from '../../lib/coverSearch';

/** What the picker chose for the edition's cover (SetEditionCoverRequest.cover). */
export type CoverChoice = { case: 'url'; value: string } | { case: 'photoId'; value: string } | { case: 'clear'; value: true };

/**
 * "Choose cover", Plex style, for one edition of the game: the photos of the edition's copies, then
 * what every enabled provider proposes for its system, then an image address to paste. Picking one
 * pins it as that edition's cover; "Automatic" clears the pin and lets the provider chain decide.
 * Two links search Google Images and SteamGridDB's site in a new tab; an image dragged from that tab, or its address pasted anywhere in the picker,
 * fills the address field, previewed by the browser before the server downloads it.
 */
export default function CoverPicker({ game, system, photosOnly = false, onPick, onClose }: {
  game: Game;
  /** The edition whose cover is chosen ('' for a game without copies). */
  system: string;
  /** Show only the photos of the edition's copies ("Use a photo"). */
  photosOnly?: boolean;
  onPick: (choice: CoverChoice) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const edition = game.editions.find((e) => e.system === system);
  // The photos of the edition's copies, each once (copies may share one).
  const photos = game.copies.filter((c) => c.effectiveSystem === system).flatMap((c) => c.photos)
    .filter((p, i, all) => all.findIndex((x) => x.id === p.id) === i);
  const [candidates, setCandidates] = useState<CoverCandidate[] | null>(photosOnly ? [] : null);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [unavailable, setUnavailable] = useState(false);
  const [error, setError] = useState('');
  const [pasted, setPasted] = useState('');
  // Images that failed to load: their tiles are left out rather than shown empty yet clickable.
  const [broken, setBroken] = useState<string[]>([]);
  const breaks = (src: string) => () => setBroken((b) => (b.includes(src) ? b : [...b, src]));
  const automatic = !edition?.coverUrl && !edition?.coverPhotoId;
  // A drop or paste that held only a data: URI (a Google thumbnail inlined): nothing to fill, but say why.
  const [onlyPreview, setOnlyPreview] = useState(false);
  const [dragging, setDragging] = useState(false);
  // The address previewed, a moment after typing stops, and the one that failed to load.
  const [previewSrc, setPreviewSrc] = useState('');
  const [failedSrc, setFailedSrc] = useState('');
  const urlInput = useRef<HTMLInputElement>(null);
  // Candidates per edition for the dialog's life: StrictMode's second effect run and switching back
  // to an edition reuse the answer instead of spending provider quota again.
  const requests = useRef(new Map<string, ReturnType<typeof coverClient.listCoverCandidates>>());
  const valid = isHttpUrl(pasted);
  const smallHint = onlyPreview || (pasted.trim() !== '' && isSmallPreview(pasted));

  const take = (found: DroppedImage) => {
    if (found.url) setPasted(found.url);
    setOnlyPreview(!found.url && found.small);
  };

  useEffect(() => {
    const id = setTimeout(() => setPreviewSrc(isHttpUrl(pasted) ? pasted.trim() : ''), 300);
    return () => clearTimeout(id);
  }, [pasted]);

  // Pasting anywhere in the picker fills the address field; in another field it is left alone, and in
  // the address field itself only a data: URI is held back (with the hint), the rest pastes as text.
  useEffect(() => {
    if (photosOnly) return;
    const onPaste = (e: ClipboardEvent) => {
      const target = e.target as HTMLElement | null;
      const editable = target?.closest('input, textarea, [contenteditable="true"]');
      if (editable && editable !== urlInput.current) return;
      const found = imageFromTransfer((type) => e.clipboardData?.getData(type) ?? '');
      if (editable && !(found.small && !found.url)) return;
      if (!found.url && !found.small) return;
      e.preventDefault();
      take(found);
    };
    document.addEventListener('paste', onPaste);
    return () => document.removeEventListener('paste', onPaste);
  }, [photosOnly]);

  // An image (or link) dragged from another tab can be dropped anywhere in the picker.
  const carriesLink = (e: DragEvent) => ['text/uri-list', 'text/plain', 'text/html'].some((type) => e.dataTransfer.types.includes(type));
  const dropHandlers = photosOnly ? {} : {
    onDragOver: (e: DragEvent) => {
      if (!carriesLink(e)) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = 'copy';
      setDragging(true);
    },
    onDragLeave: (e: DragEvent) => {
      if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDragging(false);
    },
    onDrop: (e: DragEvent) => {
      setDragging(false);
      if (!carriesLink(e)) return;
      e.preventDefault();
      take(imageFromTransfer((type) => e.dataTransfer.getData(type)));
    },
  };

  useEffect(() => {
    if (photosOnly) return;
    let current = true; // a later edition's answer, or an unmounted dialog, ignores this one
    cachedRequest(requests.current, `${game.id}\u0000${system}`, () => coverClient.listCoverCandidates({ gameId: game.id, system }))
      .then((res) => { if (current) { setCandidates(res.candidates); setWarnings(res.warnings); setUnavailable(res.providersUnavailable); } })
      .catch((e) => { if (current) setError(errorMessage(e)); });
    return () => { current = false; };
  }, [game.id, system, photosOnly]);

  return (
    <Modal wide title={system ? t('coverPicker.titleFor', { system }) : t('coverPicker.title')} onClose={onClose}>
      <div className={`cover-drop ${dragging ? 'dragging' : ''}`} {...dropHandlers}>
        {photos.length > 0 && (
          <>
            <h3>{t('coverPicker.photos')}</h3>
            <div className="posters picker-grid">
              {photos.filter((p) => !broken.includes(photoUrl(p.id, true))).map((p) => (
                <button key={p.id} className={`poster ${edition?.coverPhotoId === p.id ? 'selected' : ''}`} title={p.caption} aria-label={p.caption || t('coverPicker.photo')}
                  onClick={() => onPick({ case: 'photoId', value: p.id })}>
                  <WholeCover src={photoUrl(p.id, true)} onError={breaks(photoUrl(p.id, true))} />
                </button>
              ))}
            </div>
          </>
        )}
        {!photosOnly && (
          <>
            <p className="muted small">{t('coverPicker.intro')}</p>
            {error && <Alert tone="error">{error}</Alert>}
            {warnings.map((w) => <Alert key={w} tone="warn">{w}</Alert>)}
            {unavailable && <p><ProvidersUnavailable /></p>}
            {candidates === null && !error && <p className="muted">{t('common.loading')}</p>}
            {candidates && (
              <div className="posters picker-grid">
                <button className={`poster ${automatic ? 'selected' : ''}`} onClick={() => onPick({ case: 'clear', value: true })}>
                  <WholeCover placeholder={<div className="cover-placeholder"><span>AUTO</span></div>} />
                  <div className="poster-caption">
                    <span className="poster-title">{t('coverPicker.automatic')}</span>
                    <span className="muted small">{t('coverPicker.automaticHint')}</span>
                  </div>
                </button>
                {candidates.filter((c) => !broken.includes(proxiedImage(c.thumbUrl || c.url))).map((c) => (
                  <button key={c.url} className={`poster ${edition?.coverUrl === c.url ? 'selected' : ''}`} onClick={() => onPick({ case: 'url', value: c.url })} title={c.label}>
                    <WholeCover src={proxiedImage(c.thumbUrl || c.url)} onError={breaks(proxiedImage(c.thumbUrl || c.url))} />
                    <div className="poster-caption">
                      <span className="poster-title">{c.label || c.providerName}</span>
                      <span className="muted small">{c.providerName}</span>
                    </div>
                  </button>
                ))}
              </div>
            )}
            {candidates?.length === 0 && <p className="muted">{t('coverPicker.none')}</p>}
            <div className="row tight cover-search">
              <a className="button small-button" href={googleImagesUrl(game.title, system)} target="_blank" rel="noopener noreferrer">
                {t('coverPicker.searchGoogle')}<Icon name="external" size={14} />
              </a>
              <a className="button small-button" href={steamGridDbUrl(game.title)} target="_blank" rel="noopener noreferrer">
                {t('coverPicker.searchSteamGridDb')}<Icon name="external" size={14} />
              </a>
            </div>
            <p className="muted small cover-search-help">{t('coverPicker.findHelp')}</p>
            <form className="row tight cover-paste" onSubmit={(e) => { e.preventDefault(); if (valid) onPick({ case: 'url', value: pasted.trim() }); }}>
              <label>
                {t('game.coverUrl')}
                <input ref={urlInput} type="url" value={pasted} placeholder="https://…"
                  onChange={(e) => { setPasted(e.target.value); setOnlyPreview(false); }} />
              </label>
              {previewSrc && failedSrc !== previewSrc && (
                <div className="cover-paste-preview" aria-label={t('coverPicker.preview')} role="img">
                  <WholeCover key={previewSrc} src={previewSrc} noReferrer onError={() => setFailedSrc(previewSrc)} />
                </div>
              )}
              <button type="submit" disabled={!valid}>{t('coverPicker.paste')}</button>
            </form>
            {smallHint && <p className="muted small">{t('coverPicker.smallPreview')}</p>}
            {previewSrc && failedSrc === previewSrc && <Alert tone="warn">{t('coverPicker.previewFailed')}</Alert>}
          </>
        )}
      </div>
    </Modal>
  );
}
