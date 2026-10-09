// Small pure helpers the engine and the background use.

/** The value of param in url when url starts with prefix; '' otherwise. */
export function redirectValue(url, prefix, param) {
  if (typeof url !== 'string' || !url.startsWith(prefix)) return '';
  try { return new URL(url).searchParams.get(param) ?? ''; } catch { return ''; }
}

/** Cookies as a Cookie request header. */
export function cookieHeader(cookies) {
  return cookies.map((c) => `${c.name}=${c.value}`).join('; ');
}

/** A string field of a JSON answer, or of a form-encoded one ("a=1&b=2"); '' when missing or null. */
export function jsonField(text, field) {
  try {
    const v = JSON.parse(text)?.[field];
    return typeof v === 'string' ? v : '';
  } catch {
    for (const part of String(text).split(/[&\s]/)) {
      const [k, ...rest] = part.split('=');
      if (k === field && rest.length) return decodeURIComponent(rest.join('='));
    }
    return '';
  }
}

/** Whether a page's origin is one of the Game Vault addresses the user enabled (exact match, port included). */
export function originAllowed(origin, enabled) {
  return Array.isArray(enabled) && enabled.includes(origin);
}

/** Whether every host was confirmed before. */
export function hostsGranted(hosts, remembered) {
  return hosts.every((h) => remembered.includes(h));
}

/**
 * Whether a confirmation may be remembered for a Game Vault address: on https, or on this computer.
 * On plain http over a network, anyone on that network could inject a script into the page and
 * reuse a remembered confirmation silently, so the user confirms each sign-in instead.
 */
export function rememberAllowed(origin) {
  try {
    const u = new URL(origin);
    return u.protocol === 'https:' || ['localhost', '127.0.0.1', '[::1]'].includes(u.hostname);
  } catch {
    return false;
  }
}
