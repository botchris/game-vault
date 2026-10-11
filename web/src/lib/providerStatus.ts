type Translate = (key: string, options?: Record<string, string>) => string;

const reasonKeys: Record<string, string> = {
  quota: 'providerStatus.quota',
  rate_limited: 'providerStatus.rateLimited',
  unavailable: 'providerStatus.unavailable',
  settings: 'providerStatus.settings',
};

/** When a problem ends, relative to now, in the largest unit that fits ("in 27 days"). */
export function renewsIn(until: Date, now: Date, locale: string): string {
  const ms = until.getTime() - now.getTime();
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
  const hour = 3600 * 1000;
  if (ms >= 36 * hour) return rtf.format(Math.round(ms / (24 * hour)), 'day');
  if (ms >= 90 * 60 * 1000) return rtf.format(Math.round(ms / hour), 'hour');
  return rtf.format(Math.max(1, Math.round(ms / 60000)), 'minute');
}

/**
 * The translated reason of a provider's problem ("monthly allowance used up; renews in 27 days").
 * A kind this version does not know shows the provider's own message.
 */
export function describeStatus(status: { kind: string; message: string }, until: Date | undefined, now: Date, t: Translate,
  locale: string): string {
  const key = reasonKeys[status.kind];
  if (!key) return status.message;
  if (status.kind === 'quota' && until) return t(key, { when: renewsIn(until, now, locale) });
  return t(key);
}

/** The status of the service a provider belongs to, if it has a problem. */
export function statusFor<S extends { providerId: string; providerIds: string[] }>(statuses: S[], providerId: string): S | undefined {
  return statuses.find((s) => s.providerId === providerId || s.providerIds.includes(providerId));
}
