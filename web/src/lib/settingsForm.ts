// Rules shared by the settings dialogs of sources and providers (components/SettingsForm.tsx).

/** A consent setting holds this value once its risk is accepted. */
export const ACCEPTED = 'yes';

/** Whether a required risk is not accepted yet: testing and saving wait for it (the server refuses
 *  them too). `consents` are the consent fields of the form. */
export function consentMissing(consents: readonly { key: string; required: boolean }[], settings: Record<string, string>): boolean {
  return consents.some((f) => f.required && settings[f.key] !== ACCEPTED);
}
