// Overlapping requests whose answers replace the same state (the home page's GetHome, a refresh
// answering with the whole home): only the newest may apply its answer, so a slow older one cannot
// overwrite a newer one. Node-tested (tests/latest.test.mjs).

/** A counter of requests: each call starts one and returns whether it is still the newest. */
export function latestOnly(): () => () => boolean {
  let newest = 0;
  return () => {
    const mine = ++newest;
    return () => mine === newest;
  };
}
