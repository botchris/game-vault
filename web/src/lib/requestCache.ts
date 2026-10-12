/**
 * Returns the request cached under key, starting it with load the first time. Callers keep the map
 * for as long as answers stay valid (a dialog's life): asking again, or React's StrictMode running an
 * effect twice, reuses the request under way or its answer instead of sending another. A failed
 * request is dropped from the cache, so it can be tried again.
 */
export function cachedRequest<T>(cache: Map<string, Promise<T>>, key: string, load: () => Promise<T>): Promise<T> {
  let request = cache.get(key);
  if (!request) {
    request = load();
    cache.set(key, request);
    request.catch(() => { if (cache.get(key) === request) cache.delete(key); });
  }
  return request;
}
