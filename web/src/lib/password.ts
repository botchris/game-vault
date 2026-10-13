/** The shortest new password, in characters. The same number as the server (internal/domain/auth). */
export const MIN_PASSWORD_CHARS = 8;

/**
 * Whether a new password has enough characters. The server counts characters (runes), so this
 * counts code points: the browser's `minLength` counts UTF-16 units and would accept four emoji.
 */
export function passwordLongEnough(value: string): boolean {
  return [...value].length >= MIN_PASSWORD_CHARS;
}
