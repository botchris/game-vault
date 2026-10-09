// Talks to the Game Vault Connector browser extension through its bridge on this page
// (window.postMessage): detect it, and run a source's sign-in recipe to get a credential.

const TYPE = 'gamevault-connector';
const HELLO_TIMEOUT_MS = 1500;
// The page gives up a minute after the extension would (its default is five minutes), so the
// extension's own timeout error is the one the user sees.
const DEFAULT_SIGN_IN_SECONDS = 300;
const MARGIN_MS = 60 * 1000;

/** A sign-in recipe as Game Vault sends it (schema.SignInRecipe); the extension validates it. */
export interface Recipe {
  version: number;
  open: string;
  private?: boolean;
  timeoutSeconds?: number;
  [key: string]: unknown;
}

/** An error the extension or the page reported; code is denied, busy, invalid, unsupported, cancelled, timeout or failed. */
export class ConnectorError extends Error {
  code: string;

  constructor(code: string, message?: string) {
    super(message || code);
    this.code = code;
  }
}

interface Reply {
  op: string;
  id: string;
  version?: string;
  recipeVersion?: number;
  privateAllowed?: boolean;
  value?: string;
  code?: string;
  message?: string;
}

function request(body: Record<string, unknown>, timeoutMs: number): { reply: Promise<Reply>; id: string } {
  const id = crypto.randomUUID();
  const reply = new Promise<Reply>((resolve, reject) => {
    const onMessage = (e: MessageEvent) => {
      const d = e.data as Reply & { type?: string; dir?: string };
      if (e.source !== window || e.origin !== location.origin || d?.type !== TYPE || d.dir !== 'response' || d.id !== id) return;
      cleanup();
      resolve(d);
    };
    const timer = window.setTimeout(() => { cleanup(); reject(new ConnectorError('timeout')); }, timeoutMs);
    const cleanup = () => { window.removeEventListener('message', onMessage); window.clearTimeout(timer); };
    window.addEventListener('message', onMessage);
    window.postMessage({ ...body, type: TYPE, dir: 'request', id }, location.origin);
  });
  return { reply, id };
}

/** The extension as it introduced itself: privateAllowed says whether it may open private windows. */
export interface Connector {
  version: string;
  recipeVersion: number;
  privateAllowed: boolean;
}

/**
 * The extension's version when it is installed and enabled on this address; null otherwise. A
 * busy browser may take a moment to wake the extension up, so it asks twice before giving up.
 */
export async function detect(): Promise<Connector | null> {
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      const r = await request({ op: 'hello' }, HELLO_TIMEOUT_MS).reply;
      if (r.op !== 'hello') return null;
      return { version: r.version ?? '', recipeVersion: r.recipeVersion ?? 0, privateAllowed: !!r.privateAllowed };
    } catch {
      // No answer yet: ask once more.
    }
  }
  return null;
}

/** Runs a recipe: the extension opens the store's sign-in and resolves with the captured value. */
export function connect(source: string, field: string, recipe: Recipe): { result: Promise<string>; cancel(): void } {
  const timeoutMs = (recipe.timeoutSeconds || DEFAULT_SIGN_IN_SECONDS) * 1000 + MARGIN_MS;
  const { reply, id } = request({ op: 'connect', source, field, recipe }, timeoutMs);
  const result = reply.then((r) => {
    if (r.op === 'result' && r.value) return r.value;
    throw new ConnectorError(r.code ?? 'failed', r.message);
  });
  return { result, cancel: () => { request({ op: 'cancel', cancelId: id }, 2000).reply.catch(() => {}); } };
}
