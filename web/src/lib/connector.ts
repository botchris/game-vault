// Talks to the Game Vault Connector browser extension through its bridge on this page
// (window.postMessage): detect it, and run a source's sign-in recipe to get a credential.

const TYPE = 'gamevault-connector';
const CONNECT_TIMEOUT_MS = 11 * 60 * 1000; // a little over the extension's own maximum (10 min)

/** A sign-in recipe as Game Vault sends it (schema.SignInRecipe); the extension validates it. */
export interface Recipe {
  version: number;
  open: string;
  private?: boolean;
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

/** The extension's version when it is installed and enabled on this address; null otherwise. */
export async function detect(): Promise<{ version: string; recipeVersion: number } | null> {
  try {
    const r = await request({ op: 'hello' }, 600).reply;
    return r.op === 'hello' ? { version: r.version ?? '', recipeVersion: r.recipeVersion ?? 0 } : null;
  } catch {
    return null;
  }
}

/** Runs a recipe: the extension opens the store's sign-in and resolves with the captured value. */
export function connect(source: string, field: string, recipe: Recipe): { result: Promise<string>; cancel(): void } {
  const { reply, id } = request({ op: 'connect', source, field, recipe }, CONNECT_TIMEOUT_MS);
  const result = reply.then((r) => {
    if (r.op === 'result' && r.value) return r.value;
    throw new ConnectorError(r.code ?? 'failed', r.message);
  });
  return { result, cancel: () => { request({ op: 'cancel', cancelId: id }, 2000).reply.catch(() => {}); } };
}
