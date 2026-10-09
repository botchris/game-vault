import { Code, ConnectError, createClient, type Interceptor } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-web';
import { AuthService } from '../gen/gamevault/v1/auth_pb';
import { GameService, type Game } from '../gen/gamevault/v1/game_pb';
import { LogService } from '../gen/gamevault/v1/log_pb';
import { LookupService } from '../gen/gamevault/v1/lookup_pb';
import { MetadataService } from '../gen/gamevault/v1/metadata_pb';
import { CoverService, ProviderService } from '../gen/gamevault/v1/provider_pb';
import { SourceService } from '../gen/gamevault/v1/source_pb';
import { SystemService } from '../gen/gamevault/v1/system_pb';

/** Fired when the server says the session is gone, so the app shows the login screen again. */
export const UNAUTHENTICATED_EVENT = 'gamevault:unauthenticated';

const authWatcher: Interceptor = (next) => async (req) => {
  try {
    return await next(req);
  } catch (err) {
    if (err instanceof ConnectError && err.code === Code.Unauthenticated && !req.url.includes('/gamevault.v1.AuthService/')) {
      window.dispatchEvent(new Event(UNAUTHENTICATED_EVENT));
    }
    throw err;
  }
};

/** The UI only talks to the backend through these typed ConnectRPC clients. */
export const baseUrl = import.meta.env.VITE_API_URL || window.location.origin;
const transport = createConnectTransport({ baseUrl, interceptors: [authWatcher], fetch: (input, init) => fetch(input, { ...init, credentials: 'include' }) });

export const authClient = createClient(AuthService, transport);
export const gameClient = createClient(GameService, transport);
export const sourceClient = createClient(SourceService, transport);
export const systemClient = createClient(SystemService, transport);
export const logClient = createClient(LogService, transport);
export const providerClient = createClient(ProviderService, transport);
export const coverClient = createClient(CoverService, transport);
export const lookupClient = createClient(LookupService, transport);
export const metadataClient = createClient(MetadataService, transport);

/**
 * Cover image URL. Images are plain HTTP (not RPC) so <img> can load and cache them;
 * the updatedAt version busts the browser cache when the cover changes.
 */
export function coverUrl(g: Game): string {
  return `${baseUrl}/media/covers/${g.id}?v=${g.updatedAt?.seconds ?? 0}`;
}

/** A copy photo (or its thumbnail). Photos never change, so the URL needs no version. */
export function photoUrl(id: string, thumb = false): string {
  return `${baseUrl}/media/photos/${id}${thumb ? '/thumb' : ''}`;
}

/**
 * Provider image (thumbnail, store art) loaded through the server: many CDNs reject hotlinked
 * images. Only hosts declared by a provider are proxied.
 */
export function proxiedImage(url: string): string {
  return url ? `${baseUrl}/media/proxy?url=${encodeURIComponent(url)}` : '';
}

/**
 * Image of a game's sheet. The server returns local paths (/media/games/{id}/assets/…) served from
 * the game's folder; anything else is a remote image and goes through the proxy.
 */
export function mediaUrl(pathOrUrl: string): string {
  if (!pathOrUrl) return '';
  return pathOrUrl.startsWith('/') ? `${baseUrl}${pathOrUrl}` : proxiedImage(pathOrUrl);
}

/** Human-readable message for any error thrown by a client call. */
export function errorMessage(err: unknown): string {
  if (err instanceof ConnectError) return err.rawMessage;
  return err instanceof Error ? err.message : String(err);
}
