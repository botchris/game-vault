/** Why the provider dialog was opened: to change the settings, or to turn the provider on. */
export type DialogIntent = 'configure' | 'enable';

/**
 * Whether the provider dialog starts with "Enabled" ticked: as the provider is now, so saving its
 * settings never turns a disabled provider back on, unless the dialog was opened by switching it on
 * (it needed its settings first).
 */
export function initialEnabled(enabled: boolean, intent: DialogIntent): boolean {
  return enabled || intent === 'enable';
}
