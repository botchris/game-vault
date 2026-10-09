import {
  siBattledotnet, siEa, siEpicgames, siGogdotcom, siHumblebundle, siItchdotio, siPlaystation, siRiotgames, siRockstargames,
  siSega, siSteam, siUbisoft,
} from 'simple-icons';
import type { CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import { CopyKind, CopyStatus, isPendingKey, kindKey, type Game } from '../lib/model';

/**
 * A glyph: a filled path (Simple Icons, CC0, on a 24×24 box unless viewBox says otherwise) or our
 * own stroke drawing for brands no icon set has.
 */
type Glyph = { fill: string; viewBox?: string } | { stroke: string };

interface PlatformLook {
  glyph?: Glyph;
  /** Short text next to the glyph (console generation, or the name when there is no glyph). */
  label?: string;
  bg: string;
  fg?: string;
}

const XBOX: Glyph = { stroke: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM8 8l8 8M16 8l-8 8' };
const SWITCH: Glyph = { fill: 'M9.5 2H7a5 5 0 0 0-5 5v10a5 5 0 0 0 5 5h2.5zM6.2 7.4a1.6 1.6 0 1 0 0 .01zM14.5 2v20H17a5 5 0 0 0 5-5V7a5 5 0 0 0-5-5zm3.3 11a1.6 1.6 0 1 1 0 .01z' };
const PC: Glyph = { stroke: 'M3 4h18v12H3zM8 20h8M12 16v4' };
const STORE: Glyph = { stroke: 'M5 8h14l-1 12H6zM9 8V6a3 3 0 0 1 6 0v2' };
// Simple Icons has no Amazon logo; this one is Font Awesome Free 6.7.2's "amazon" brand icon
// (https://fontawesome.com, CC BY 4.0).
const AMAZON: Glyph = {
  viewBox: '0 0 448 512',
  fill: 'M257.2 162.7c-48.7 1.8-169.5 15.5-169.5 117.5 0 109.5 138.3 114 183.5 43.2 6.5 10.2 35.4 37.5 45.3 46.8l56.8-56S341 288.9 341 261.4V114.3C341 89 316.5 32 228.7 32 140.7 32 94 87 94 136.3l73.5 6.8c16.3-49.5 54.2-49.5 54.2-49.5 40.7-.1 35.5 29.8 35.5 69.1zm0 86.8c0 80-84.2 68-84.2 17.2 0-47.2 50.5-56.7 84.2-57.8v40.6zm136 163.5c-7.7 10-70 67-174.5 67S34.2 408.5 9.7 379c-6.8-7.7 1-11.3 5.5-8.3C88.5 415.2 203 488.5 387.7 401c7.5-3.7 13.3 2 5.5 12zm39.8 2.2c-6.5 15.8-16 26.8-21.2 31-5.5 4.5-9.5 2.7-6.5-3.8s19.3-46.5 12.7-55c-6.5-8.3-37-4.3-48-3.2-10.8 1-13 2-14-.3-2.3-5.7 21.7-15.5 37.5-17.5 15.7-1.8 41-.8 46 5.7 3.7 5.1 0 27.1-6.5 43.1z',
};
// No icon set has Fanatical's logo: our own "F" monogram, not a copy of it.
const FANATICAL: Glyph = { fill: 'M6 3h12.5v4.2H10.8v3.4h6.6v4.2h-6.6V21H6z' };
const KEY: Glyph = { stroke: 'M10.5 13.5 20 4M17 7l2.5 2.5M14.5 9.5 16.5 11.5M8 21a4.5 4.5 0 1 0 0-9 4.5 4.5 0 0 0 0 9z' };

const si = (icon: { path: string }): Glyph => ({ fill: icon.path });
const PS = si(siPlaystation);

const NINTENDO_BG = '#e60012';
const XBOX_BG = '#107c10';
const PS_BG = '#0070d1';

/** How each platform name used by Game Vault looks. Unknown names fall back to a neutral chip. */
const LOOKS: Record<string, PlatformLook> = {
  'steam': { glyph: si(siSteam), bg: '#1b2838' },
  'epic games': { glyph: si(siEpicgames), bg: '#2a2a2a' },
  'gog': { glyph: si(siGogdotcom), bg: '#86328a' },
  'ea app': { glyph: si(siEa), bg: '#e4002b' },
  'battle.net': { glyph: si(siBattledotnet), bg: '#148eff' },
  'ubisoft connect': { glyph: si(siUbisoft), bg: '#1a1a1a' },
  'rockstar': { glyph: si(siRockstargames), bg: '#fcaf17', fg: '#111' },
  'riot': { glyph: si(siRiotgames), bg: '#eb0029' },
  'itch.io': { glyph: si(siItchdotio), bg: '#fa5c5c' },
  'battlestate (tarkov)': { label: 'EFT', bg: '#2d2f28' },
  'amazon games': { glyph: AMAZON, bg: '#232f3e' },
  'microsoft store / xbox': { glyph: XBOX, bg: XBOX_BG },
  'playstation store': { glyph: PS, bg: PS_BG },
  'nintendo eshop': { glyph: SWITCH, label: 'eShop', bg: NINTENDO_BG },
  'pc': { glyph: PC, label: 'PC', bg: '#4b5568' },
  'ps5': { glyph: PS, label: 'PS5', bg: PS_BG },
  'ps4': { glyph: PS, label: 'PS4', bg: PS_BG },
  'ps3': { glyph: PS, label: 'PS3', bg: PS_BG },
  'ps2': { glyph: PS, label: 'PS2', bg: PS_BG },
  'ps1': { glyph: PS, label: 'PS1', bg: PS_BG },
  'psp': { glyph: PS, label: 'PSP', bg: PS_BG },
  'ps vita': { glyph: PS, label: 'Vita', bg: PS_BG },
  'xbox series': { glyph: XBOX, label: 'Series', bg: XBOX_BG },
  'xbox one': { glyph: XBOX, label: 'One', bg: XBOX_BG },
  'xbox 360': { glyph: XBOX, label: '360', bg: XBOX_BG },
  'xbox': { glyph: XBOX, bg: XBOX_BG },
  'switch': { glyph: SWITCH, label: 'Switch', bg: NINTENDO_BG },
  'wii u': { label: 'Wii U', bg: NINTENDO_BG },
  'wii': { label: 'Wii', bg: NINTENDO_BG },
  'gamecube': { label: 'GC', bg: NINTENDO_BG },
  'n64': { label: 'N64', bg: NINTENDO_BG },
  '3ds': { label: '3DS', bg: NINTENDO_BG },
  'ds': { label: 'DS', bg: NINTENDO_BG },
  'game boy': { label: 'GB', bg: NINTENDO_BG },
  'sega': { glyph: si(siSega), bg: '#0089cf' },
};

export function platformLook(platform: string): PlatformLook {
  return LOOKS[platform.trim().toLowerCase()] ?? { glyph: STORE, label: platform, bg: '#4b5568' };
}

function GlyphSvg({ glyph, size }: { glyph: Glyph; size: number }) {
  return 'fill' in glyph
    ? <svg width={size} height={size} viewBox={glyph.viewBox ?? '0 0 24 24'} fill="currentColor" aria-hidden="true"><path d={glyph.fill} /></svg>
    : <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.4}
      strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={glyph.stroke} /></svg>;
}

/** What you have on a platform: owned (library or physical) beats a pending key. */
export type Holding = 'owned' | 'key';

export interface PlatformHolding {
  platform: string;
  holding: Holding;
  kind: CopyKind;
}

const KIND_ORDER = { [CopyKind.PHYSICAL]: 0, [CopyKind.LIBRARY]: 1, [CopyKind.KEY]: 2, [CopyKind.UNSPECIFIED]: 3 };

/**
 * The platforms a game is available on, one entry per platform. Copies you no longer have (sold,
 * gifted, expired keys) are left out; a redeemed key counts as owned.
 */
export function platformHoldings(game: Game): PlatformHolding[] {
  const by = new Map<string, PlatformHolding>();
  for (const c of game.copies) {
    const d = c.details;
    if (!d) continue;
    if ([CopyStatus.SOLD, CopyStatus.GIFTED, CopyStatus.EXPIRED].includes(d.status)) continue;
    const platform = d.platform;
    if (!platform) continue;
    const holding: Holding = isPendingKey(c) ? 'key' : 'owned';
    const k = platform.toLowerCase();
    const prev = by.get(k);
    if (!prev || (prev.holding === 'key' && holding === 'owned')) by.set(k, { platform, holding, kind: d.kind });
  }
  return [...by.values()].sort((a, b) => (a.holding === b.holding ? 0 : a.holding === 'owned' ? -1 : 1) || KIND_ORDER[a.kind] - KIND_ORDER[b.kind]);
}

/** A platform logo chip. `full` also prints the platform name (for the game sheet). With
 * onSelect it is a button (used to filter the library by that platform). */
export function PlatformBadge({ platform, holding = 'owned', kind, full = false, onSelect, active = false }: {
  platform: string;
  holding?: Holding;
  kind?: CopyKind;
  full?: boolean;
  onSelect?: (platform: string) => void;
  active?: boolean;
}) {
  const { t } = useTranslation();
  const look = platformLook(platform);
  const label = full ? platform : look.label;
  const what = holding === 'key' ? t('platform.pendingKey') : kind !== undefined ? t(`kind.${kindKey(kind)}`) : '';
  const description = what ? `${platform} · ${what}` : platform;
  const className = `pbadge ${holding === 'key' ? 'is-key' : ''} ${label ? '' : 'icon-only'} ${active ? 'active' : ''}`;
  const style = { '--pb': look.bg, '--pf': look.fg ?? '#fff' } as CSSProperties;
  const body = (
    <>
      {look.glyph && <GlyphSvg glyph={look.glyph} size={13} />}
      {label && <span className="pbadge-label" aria-hidden="true">{label}</span>}
      {holding === 'key' && <span className="pbadge-key"><GlyphSvg glyph={KEY} size={11} /></span>}
    </>
  );
  if (!onSelect) {
    return <span className={className} style={style} title={description}>{body}<span className="sr-only">{description}</span></span>;
  }
  const action = active ? t('platform.clearFilter') : t('platform.filterBy', { platform });
  return (
    <button type="button" className={className} style={style} title={`${description}\n${action}`} aria-label={`${description}. ${action}`}
      aria-pressed={active} onClick={(e) => { e.stopPropagation(); onSelect(platform); }}>
      {body}
    </button>
  );
}

/** The badges of a game, capped with a "+N" chip. */
export function PlatformBadges({ game, max = 3, onSelect, active = [] }: {
  game: Game;
  max?: number;
  onSelect?: (platform: string) => void;
  active?: string[];
}) {
  const all = platformHoldings(game);
  const shown = all.length > max ? all.slice(0, max - 1) : all;
  const rest = all.length - shown.length;
  return (
    <span className="pbadges">
      {shown.map((h) => <PlatformBadge key={h.platform} {...h} onSelect={onSelect} active={active.includes(h.platform)} />)}
      {rest > 0 && <span className="pbadge more" title={all.slice(shown.length).map((h) => h.platform).join(', ')}>+{rest}</span>}
    </span>
  );
}

/** The store behind each source type, by the platform name its copies use (Humble has its own). */
const SOURCE_LOOKS: Record<string, PlatformLook> = {
  humble: { glyph: si(siHumblebundle), bg: '#cc2929' },
  steam: platformLook('Steam'),
  epic: platformLook('Epic Games'),
  gog: platformLook('GOG'),
  battlenet: platformLook('Battle.net'),
  eaapp: platformLook('EA App'),
  ubisoft: platformLook('Ubisoft Connect'),
  xbox: platformLook('Microsoft Store / Xbox'),
  playstation: { glyph: PS, bg: PS_BG },
  amazon: platformLook('Amazon Games'),
  fanatical: { glyph: FANATICAL, bg: '#ff9800', fg: '#111' },
};

/** A source's store logo in its brand colour, as a rounded tile. */
export function SourceLogo({ type, size = 40 }: { type: string; size?: number }) {
  const look = SOURCE_LOOKS[type] ?? { glyph: STORE, bg: '#4b5568' };
  const style = { '--pb': look.bg, '--pf': look.fg ?? '#fff', width: size, height: size } as CSSProperties;
  return (
    <span className="source-logo" style={style} aria-hidden="true">
      {look.glyph && <GlyphSvg glyph={look.glyph} size={Math.round(size * 0.5)} />}
    </span>
  );
}
