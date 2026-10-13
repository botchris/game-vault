// "Is this address on this computer?", defined once: remembering a sign-in confirmation and sending
// the ownership token both depend on it, so hardening one hardens the other.

/** Whether a URL hostname is this computer: localhost, any of 127.0.0.0/8, or ::1. */
export function isLoopback(hostname) {
  return hostname === 'localhost' || hostname === '[::1]' || /^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(String(hostname));
}

/**
 * Whether what a page at `origin` sends or receives stays private from the network: https, or
 * plain http to this computer. Plain http over a network can be read, and its pages altered, by
 * anyone on that network.
 */
export function isPrivateTransport(origin) {
  try {
    const url = new URL(origin);
    return url.protocol === 'https:' || (url.protocol === 'http:' && isLoopback(url.hostname));
  } catch {
    return false;
  }
}
