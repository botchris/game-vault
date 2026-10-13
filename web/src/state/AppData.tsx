import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, fieldClient, gameClient, sourceClient, systemClient } from '../api/client';
import type { FieldDefinition } from '../gen/gamevault/v1/field_pb';
import type { Game } from '../gen/gamevault/v1/game_pb';
import type { Source, SourceType } from '../gen/gamevault/v1/source_pb';

interface AppData {
  games: Game[];
  sources: Source[];
  /** Custom field definitions, in display order. */
  fields: FieldDefinition[];
  sourceTypes: SourceType[];
  loading: boolean;
  error: string;
  /** Games whose details (genres, year) are already downloaded in the UI language. */
  detailsCached: number;
  /** The user's preferences; `currency` is '' until one is saved. */
  preferences: Preferences;
  reloadGames: () => Promise<void>;
  reloadSources: () => Promise<void>;
  reloadFields: () => Promise<void>;
  reloadPreferences: () => Promise<void>;
  /** Replace (or add) a game after a mutation, without refetching everything. */
  putGame: (g: Game) => void;
  dropGame: (id: string) => void;
  sourceName: (id: string) => string;
}

/** The preferences the UI uses. */
export interface Preferences {
  /** Default currency of new prices (ISO 4217); '' until the user saves one. */
  currency: string;
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
  const [preferences, setPreferences] = useState<Preferences>({ currency: '' });
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

  // Loaded once for every component that needs them (the money inputs, System), and again when
  // System saves them.
  const reloadPreferences = useCallback(async () => {
    try {
      const res = await systemClient.getPreferences({});
      setPreferences({ currency: res.preferences?.currency ?? '' });
    } catch {
      // Without them the region's currency is proposed, as before one is saved.
    }
  }, []);

  useEffect(() => {
    Promise.all([reloadGames(), reloadSources(), reloadFields(), reloadPreferences()]).finally(() => setLoading(false));
  }, [reloadGames, reloadSources, reloadFields, reloadPreferences]);

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
      games, sources, fields, sourceTypes, loading, error, detailsCached, preferences,
      reloadGames, reloadSources, reloadFields, reloadPreferences, putGame, dropGame,
      sourceName: (id) => names.get(id) ?? '',
    };
  }, [games, sources, fields, sourceTypes, loading, error, detailsCached, preferences, reloadGames, reloadSources, reloadFields,
    reloadPreferences, putGame, dropGame]);

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAppData(): AppData {
  const v = useContext(Ctx);
  if (!v) throw new Error('useAppData must be used inside AppDataProvider');
  return v;
}
