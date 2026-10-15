import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { catalogClient, errorMessage, fieldClient, gameClient, sourceClient, systemClient } from '../api/client';
import type { FieldDefinition } from '../gen/gamevault/v1/field_pb';
import type { Game } from '../gen/gamevault/v1/game_pb';
import type { Source, SourceType } from '../gen/gamevault/v1/source_pb';
import type { PreferencesState } from '../lib/preferences';
import type { SystemInfo } from '../lib/systems';

interface AppData {
  games: Game[];
  sources: Source[];
  /** Custom field definitions, in display order. */
  fields: FieldDefinition[];
  sourceTypes: SourceType[];
  /** The catalog of game systems, in display order; empty until it is loaded. */
  systems: SystemInfo[];
  loading: boolean;
  error: string;
  /** Games whose details (genres, year) are already downloaded in the UI language. */
  detailsCached: number;
  /** The user's preferences: unknown (with the error) until they are read. */
  preferences: PreferencesState;
  reloadGames: () => Promise<void>;
  reloadSources: () => Promise<void>;
  reloadFields: () => Promise<void>;
  reloadPreferences: () => Promise<void>;
  /** Replace (or add) a game after a mutation, without refetching everything. */
  putGame: (g: Game) => void;
  dropGame: (id: string) => void;
  sourceName: (id: string) => string;
}

const Ctx = createContext<AppData | null>(null);

/** Holds the catalog and sources fetched from the backend, shared by every page. */
export function AppDataProvider({ children }: { children: ReactNode }) {
  const { i18n } = useTranslation();
  const language = i18n.resolvedLanguage ?? 'en';
  const [games, setGames] = useState<Game[]>([]);
  const [detailsCached, setDetailsCached] = useState(0);
  const [sources, setSources] = useState<Source[]>([]);
  const [fields, setFields] = useState<FieldDefinition[]>([]);
  const [sourceTypes, setSourceTypes] = useState<SourceType[]>([]);
  const [systems, setSystems] = useState<SystemInfo[]>([]);
  const [preferences, setPreferences] = useState<PreferencesState>({ loaded: false, error: '' });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const reloadGames = useCallback(async () => {
    try {
      const res = await gameClient.listGames({ language });
      setGames(res.games);
      setDetailsCached(res.detailsCached);
      setError('');
    } catch (e) {
      setError(errorMessage(e));
    }
  }, [language]);

  const reloadSources = useCallback(async () => {
    try {
      const [list, types] = await Promise.all([sourceClient.listSources({}), sourceClient.listSourceTypes({})]);
      setSources(list.sources);
      setSourceTypes(types.types);
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  const reloadFields = useCallback(async () => {
    try {
      setFields((await fieldClient.listFields({})).fields);
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  // The catalog never changes while the server runs, so it is loaded once.
  const loadSystems = useCallback(async () => {
    try {
      const res = await catalogClient.listSystems({});
      setSystems(res.systems.map((s) => ({ name: s.name, maker: s.maker, kind: s.kind, year: s.year, aliases: s.aliases, short: s.short })));
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  // Loaded once for every component that needs them (the money inputs, System), and again when
  // System saves them.
  const reloadPreferences = useCallback(async () => {
    try {
      const res = await systemClient.getPreferences({});
      setPreferences({ loaded: true, currency: res.preferences?.currency ?? '' });
    } catch (e) {
      // The money inputs propose the region's currency meanwhile; System says the saved one is
      // unknown instead of offering to save over it.
      setPreferences({ loaded: false, error: errorMessage(e) });
    }
  }, []);

  useEffect(() => {
    Promise.all([reloadGames(), reloadSources(), reloadFields(), reloadPreferences(), loadSystems()]).finally(() => setLoading(false));
  }, [reloadGames, reloadSources, reloadFields, reloadPreferences, loadSystems]);

  const putGame = useCallback((g: Game) => {
    setGames((list) => {
      const i = list.findIndex((x) => x.id === g.id);
      if (i < 0) return [...list, g];
      const next = list.slice();
      next[i] = g;
      return next;
    });
  }, []);

  const dropGame = useCallback((id: string) => setGames((list) => list.filter((g) => g.id !== id)), []);

  const value = useMemo<AppData>(() => {
    const names = new Map(sources.map((s) => [s.id, s.name]));
    return {
      games, sources, fields, sourceTypes, systems, loading, error, detailsCached, preferences,
      reloadGames, reloadSources, reloadFields, reloadPreferences, putGame, dropGame,
      sourceName: (id) => names.get(id) ?? '',
    };
  }, [games, sources, fields, sourceTypes, systems, loading, error, detailsCached, preferences, reloadGames, reloadSources, reloadFields,
    reloadPreferences, putGame, dropGame]);

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAppData(): AppData {
  const v = useContext(Ctx);
  if (!v) throw new Error('useAppData must be used inside AppDataProvider');
  return v;
}
