import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, gameClient, sourceClient } from '../api/client';
import type { Game } from '../gen/gamevault/v1/game_pb';
import type { Source, SourceType } from '../gen/gamevault/v1/source_pb';

interface AppData {
  games: Game[];
  sources: Source[];
  sourceTypes: SourceType[];
  secretPlaceholder: string;
  loading: boolean;
  error: string;
  /** Games whose details (genres, year) are already downloaded in the UI language. */
  detailsCached: number;
  reloadGames: () => Promise<void>;
  reloadSources: () => Promise<void>;
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
  const [sourceTypes, setSourceTypes] = useState<SourceType[]>([]);
  const [secretPlaceholder, setSecretPlaceholder] = useState('');
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
      setSecretPlaceholder(types.secretPlaceholder);
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    Promise.all([reloadGames(), reloadSources()]).finally(() => setLoading(false));
  }, [reloadGames, reloadSources]);

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
      games, sources, sourceTypes, secretPlaceholder, loading, error, detailsCached,
      reloadGames, reloadSources, putGame, dropGame,
      sourceName: (id) => names.get(id) ?? '',
    };
  }, [games, sources, sourceTypes, secretPlaceholder, loading, error, detailsCached, reloadGames, reloadSources, putGame, dropGame]);

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAppData(): AppData {
  const v = useContext(Ctx);
  if (!v) throw new Error('useAppData must be used inside AppDataProvider');
  return v;
}
