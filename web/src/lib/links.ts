/**
 * Returns the address when it is an absolute https URL, undefined otherwise. Addresses that come
 * from providers (an article's page, a license) only become links when they are https, so a
 * `javascript:` or plain-http value can never end up in an href.
 */
export function httpsUrl(value: string | undefined): string | undefined {
  if (!value) return undefined;
  try {
    return new URL(value).protocol === 'https:' ? value : undefined;
  } catch {
    return undefined;
  }
}
