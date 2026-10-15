// Pure helpers of Settings → Notifications: the events grouped as the dialog lists them, the
// group's "All" box, and the state a connection's row shows. Node-tested (tests/notifications.test.mjs).

/** The groups in the order the dialog lists them; a group this version does not know goes last. */
const GROUP_ORDER = ['activity', 'attention', 'security'];

/** A group of events and its kinds, in the catalog's order. */
export interface EventGroup {
  group: string;
  kinds: string[];
}

/** The events by group (activity → attention → security, unknown ones after), each group's kinds
 *  in the order the server lists them; groups with no event are left out. */
export function groupEvents(events: { kind: string; group: string }[]): EventGroup[] {
  const groups: EventGroup[] = [];
  for (const e of events) {
    const g = groups.find((x) => x.group === e.group);
    if (g) g.kinds.push(e.kind);
    else groups.push({ group: e.group, kinds: [e.kind] });
  }
  const rank = (group: string) => {
    const i = GROUP_ORDER.indexOf(group);
    return i < 0 ? GROUP_ORDER.length : i;
  };
  return groups.sort((a, b) => rank(a.group) - rank(b.group));
}

/** The selection after a group's "All" box: every kind of the group added (on) or removed (off),
 *  the other groups' kinds kept as they were. */
export function toggleGroup(selected: string[], kinds: string[], on: boolean): string[] {
  if (!on) return selected.filter((k) => !kinds.includes(k));
  return [...selected, ...kinds.filter((k) => !selected.includes(k))];
}

/** What a connection's row says. */
export type ConnectionState = 'working' | 'paused' | 'off' | 'untested';

/** A connection's state. Paused comes first even when it is off: Health reports a paused connection
 *  as an error, and the row must show why. Untested until something was delivered through it. */
export function connectionState(c: { enabled: boolean; pausedReason: string }, lastDelivery?: { state: string }): ConnectionState {
  if (c.pausedReason) return 'paused';
  if (!c.enabled) return 'off';
  return lastDelivery ? 'working' : 'untested';
}
