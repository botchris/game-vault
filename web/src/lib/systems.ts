// The catalog of game systems as the web uses it: lookups and the grouping of the system picker.
// The catalog itself comes from CatalogService (AppData.systems). Structural types only, so Node
// tests run this file as is (no generated code, no TypeScript enums).

/** One system of the catalog, as the picker and the lookups need it. */
export interface SystemInfo {
  name: string;
  maker: string;
  kind: string;
  year: number;
  aliases: string[];
  /** The badge label; empty means the name. */
  short: string;
}

/** The comparison form of a system name: lower case, without spaces, hyphens or dots. Mirrors game.SystemKey in the server (web/tests/systemKey.cases.json is shared). */
export function systemKey(s: string): string {
  return s.toLowerCase().replace(/[\s.-]/gu, '');
}

/** The catalog system whose name or alias is `text`, ignoring case, spaces, hyphens and dots. */
export function findSystem(catalog: readonly SystemInfo[], text: string): SystemInfo | undefined {
  const k = systemKey(text);
  if (!k) return undefined;
  return catalog.find((s) => systemKey(s.name) === k) ?? catalog.find((s) => s.aliases.some((a) => systemKey(a) === k));
}

/** One line of the system picker. */
export interface PickerOption {
  name: string;
  /** The alias that matched the query, when the name did not. */
  alias?: string;
  maker: string;
}

/** A group of the system picker; a null maker is the user's own systems ("In your collection"). */
export interface PickerGroup {
  maker: string | null;
  options: PickerOption[];
}

/**
 * The groups the system picker shows. Without a query: the systems the user owns, then every
 * system under its maker, in catalog order. With one: only the systems whose name or alias
 * contains it, by maker (the search spans everything once, so there is no owned group).
 */
export function pickerGroups(catalog: readonly SystemInfo[], owned: readonly string[], query: string): PickerGroup[] {
  const q = systemKey(query);
  const byMaker = new Map<string, PickerOption[]>();

  for (const s of catalog) {
    let option: PickerOption | undefined;
    if (!q) {
      option = { name: s.name, maker: s.maker };
    } else if (systemKey(s.name).includes(q)) {
      option = { name: s.name, maker: s.maker };
    } else {
      const alias = s.aliases.find((a) => systemKey(a).includes(q));
      if (alias !== undefined) option = { name: s.name, alias, maker: s.maker };
    }
    if (!option) continue;
    const list = byMaker.get(s.maker);
    if (list) list.push(option);
    else byMaker.set(s.maker, [option]);
  }

  const groups: PickerGroup[] = [...byMaker].map(([maker, options]) => ({ maker, options }));
  if (q || owned.length === 0) return groups;

  const own = owned.map((name) => ({ name, maker: findSystem(catalog, name)?.maker ?? '' }));
  return [{ maker: null, options: own }, ...groups];
}

/** The user's system names grouped by maker, makers and systems in catalog order; names the catalog does not know go last under an empty maker. */
export function groupByMaker(names: readonly string[], catalog: readonly SystemInfo[]): { maker: string; names: string[] }[] {
  // Several names can share a key ("GameBoy" and "Game Boy"): each stays its own entry.
  const wanted = new Map<string, string[]>();
  for (const n of names) wanted.set(systemKey(n), [...(wanted.get(systemKey(n)) ?? []), n]);
  const byMaker = new Map<string, string[]>();
  for (const s of catalog) {
    const key = systemKey(s.name);
    const found = wanted.get(key);
    if (found === undefined) continue;
    wanted.delete(key);
    const list = byMaker.get(s.maker);
    if (list) list.push(...found);
    else byMaker.set(s.maker, [...found]);
  }
  const groups = [...byMaker].map(([maker, list]) => ({ maker, names: list }));
  // Aliases (a name the user typed that the catalog knows by another) count as catalog systems.
  const others: string[] = [];
  for (const name of [...wanted.values()].flat()) {
    const found = findSystem(catalog, name);
    const g = found && groups.find((x) => x.maker === found.maker);
    if (g) g.names.push(name);
    else if (found) groups.push({ maker: found.maker, names: [name] });
    else others.push(name);
  }
  return others.length ? [...groups, { maker: '', names: others }] : groups;
}
