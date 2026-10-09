// Recipe validation. The extension trusts nothing it receives: every recipe is checked here
// before anything opens, with the same rules as Game Vault's schema.SignInRecipe.Validate.

export const RECIPE_VERSION = 1;

const KINDS = ['cookie', 'cookies', 'storage', 'redirect', 'fetch'];
const REQUIRED = { cookie: ['url', 'name'], cookies: ['url'], storage: ['origin', 'key'], redirect: ['prefix', 'param'], fetch: ['url', 'field'] };
const ADDRESS = { cookie: 'url', cookies: 'url', storage: 'origin', redirect: 'prefix', fetch: 'url' };
const HOST = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/;

/** A recipe the extension refuses; code is 'invalid' or 'unsupported' (newer than this extension). */
export class RecipeError extends Error {
  constructor(message, code = 'invalid') {
    super(message);
    this.name = 'RecipeError';
    this.code = code;
  }
}

function httpsHost(address) {
  let u;
  try { u = new URL(address); } catch { throw new RecipeError(`not an address: ${address}`); }
  if (u.protocol !== 'https:') throw new RecipeError(`not an https address: ${address}`);
  return u.hostname.toLowerCase();
}

/** Checks a recipe and returns what running it needs: its capture kind, its hosts, its timeout. */
export function validate(recipe) {
  if (!recipe || typeof recipe !== 'object') throw new RecipeError('no recipe');
  if (!Number.isInteger(recipe.version) || recipe.version < 1) throw new RecipeError('no recipe version');
  if (recipe.version > RECIPE_VERSION) throw new RecipeError('this recipe needs a newer extension', 'unsupported');
  const seconds = recipe.timeoutSeconds ?? 0;
  if (!Number.isInteger(seconds) || seconds < 0 || seconds > 600) throw new RecipeError('timeout out of range');

  const hosts = [httpsHost(recipe.open)];
  for (const h of recipe.hosts ?? []) {
    if (typeof h !== 'string' || !HOST.test(h)) throw new RecipeError(`not a host name: ${h}`);
    if (!hosts.includes(h)) hosts.push(h);
  }

  const kinds = KINDS.filter((k) => recipe.capture?.[k]);
  if (kinds.length !== 1) throw new RecipeError('a recipe needs exactly one capture');
  const kind = kinds[0];
  const c = recipe.capture[kind];
  for (const f of REQUIRED[kind]) if (typeof c[f] !== 'string' || !c[f]) throw new RecipeError(`the ${kind} capture needs ${f}`);
  // A redirect is its own condition: a `when` on it would never be checked.
  if (kind === 'redirect' && recipe.when) throw new RecipeError('a redirect recipe takes no condition');
  // The extension's own requests carry the normal window's session, not the private one's.
  if (recipe.private && (kind === 'fetch' || recipe.when?.fetch)) throw new RecipeError('a private recipe cannot fetch');

  const addresses = [c[ADDRESS[kind]]];
  if (recipe.when?.urlPrefix) addresses.push(recipe.when.urlPrefix);
  if (recipe.when?.fetch) {
    if (!recipe.when.fetch.field) throw new RecipeError('the condition needs a field');
    addresses.push(recipe.when.fetch.url);
  }
  for (const a of addresses) {
    const h = httpsHost(a);
    if (!hosts.includes(h)) throw new RecipeError(`the recipe reads ${h}, which it neither opens nor lists`);
  }

  return { kind, hosts, timeoutMs: (seconds || 300) * 1000 };
}
