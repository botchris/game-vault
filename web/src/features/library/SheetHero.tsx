import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { gameClient } from '../../api/client';
import { Cover } from '../../components/Cover';
import { Icon } from '../../components/Icon';
import { PlatformBadge, platformHoldings } from '../../components/PlatformBadge';
import type { GameDetails } from '../../gen/gamevault/v1/metadata_pb';
import type { Game } from '../../lib/model';
import { useAppData } from '../../state/AppData';
import { SheetFacts } from './GameSheet';
import PlayControls from './PlayControls';
import type { GamePatch, SheetActions, SheetNav } from './sheet';

/** The game sheet's header: the edition's cover, the title, edition chips, platform badges, play
 *  status and rating, genres and the facts from the metadata providers. */
export default function SheetHero({ game, current, details, nav, actions, onClose, onPlatform, onShowEdition, onChange }: {
  game: Game;
  /** The edition the sheet shows ('' for a game without copies). */
  current: string;
  details: GameDetails | null;
  nav?: SheetNav;
  actions: SheetActions;
  onClose: () => void;
  onPlatform?: (platform: string) => void;
  onShowEdition: (system: string) => void;
  /** Saves a change to the game (play status, rating). */
  onChange: (patch: GamePatch) => Promise<unknown>;
}) {
  const { t } = useTranslation();
  const { putGame } = useAppData();
  const { busy, run, openDialog } = actions;
  const edition = game.editions.find((e) => e.system === current);
  // The system whose box the hero shows (null: none), to say when it is borrowed.
  const [shownBox, setShownBox] = useState<string | null>(current);
  const borrowed = shownBox !== current;
  const holdings = platformHoldings(game);
  const editionPhotos = game.copies.filter((c) => c.effectiveSystem === current).flatMap((c) => c.photos);
  const release = details?.releaseDate.match(/\b(19|20)\d\d\b/)?.[0] ?? (game.releaseYear || '');
  const genres = details?.genres.length ? details.genres : game.genres;

  return (
    <header className="hero">
      <div className="hero-backdrop" aria-hidden="true"><Cover game={game} system={current} /></div>
      <div className="hero-actions">
        {nav && (
          <>
            <button className="icon-button" onClick={nav.previous} disabled={!nav.previous} aria-label={t('game.previousGame')} title={t('game.previousGameKey')}><Icon name="prev" size={20} /></button>
            <button className="icon-button" onClick={nav.next} disabled={!nav.next} aria-label={t('game.nextGame')} title={t('game.nextGameKey')}><Icon name="next" size={20} /></button>
          </>
        )}
        <button className="icon-button" onClick={onClose} aria-label={t('common.close')}><Icon name="close" size={20} /></button>
      </div>
      <div className="hero-cover">
        <Cover game={game} system={current} onShown={setShownBox} whole />
        {current && borrowed && (
          <p className="cover-note">{shownBox ? t('edition.borrowedHelp', { system: shownBox }) : t('edition.noBox')}</p>
        )}
        <button type="button" className="ghost small-button" onClick={() => openDialog({ type: 'coverPicker' })}>
          <Icon name="image" size={16} />{t('coverPicker.open')}
        </button>
        {current && borrowed && editionPhotos.length > 0 && (
          <button type="button" className="ghost small-button" onClick={() => openDialog({ type: 'coverPicker', photosOnly: true })}>
            <Icon name="image" size={16} />{t('edition.usePhoto')}
          </button>
        )}
      </div>
      <div className="hero-body">
        <h2 id="sheet-title" className="hero-title">{game.title}</h2>
        {game.editions.length >= 2 && (
          <div className="edition-chips" role="group" aria-label={t('edition.chips')}>
            {game.editions.map((e) => (
              <button key={e.system} type="button" className={`edition-chip ${e.system === current ? 'active' : ''}`}
                aria-pressed={e.system === current} onClick={() => onShowEdition(e.system)}>{e.system}</button>
            ))}
          </div>
        )}
        <div className="hero-line">
          {release && <span className="hero-year">{release}</span>}
          {/* The edition's system first (a filter badge when a copy's platform names it, like a
              disc's), then the other store or console badges. */}
          {current && !holdings.some((h) => h.platform === current) && <PlatformBadge platform={current} full />}
          {[...holdings.filter((h) => h.platform === current), ...holdings.filter((h) => h.platform !== current)]
            .map((h) => <PlatformBadge key={h.platform} {...h} full onSelect={onPlatform} />)}
          {/* With one edition, saying it is the game's cover tells nothing. */}
          {game.editions.length >= 2 && edition && (edition.main
            ? <span className="badge edition-main">{t('edition.isMain')}</span>
            : (
              <button type="button" className="link edition-main" disabled={busy}
                onClick={() => run(async () => putGame((await gameClient.setMainEdition({ gameId: game.id, system: current })).game!))}>
                {t('edition.useAsMain')}
              </button>
            ))}
        </div>
        <PlayControls game={game} busy={busy} onChange={onChange} />
        {genres.length > 0 && <div className="genres">{genres.map((g) => <span key={g} className="genre">{g}</span>)}</div>}
        <SheetFacts details={details} />
      </div>
    </header>
  );
}
