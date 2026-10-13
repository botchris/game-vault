import { useTranslation } from 'react-i18next';
import { amountInput, readAmount } from './money';

/**
 * The reading of an amount being typed in `currency` (the copy form's price, a money field): the
 * caller keeps the text, since one form owns it and a field resyncs it with its saved value.
 * `minor` is undefined while the text is empty and null while it is not an amount.
 */
export function useAmountDraft(currency: string, text: string) {
  const { i18n } = useTranslation();
  const minor = readAmount(text, currency);
  return {
    minor,
    invalid: minor === null,
    /** What an amount looks like in this currency and language ("29,95"). */
    placeholder: amountInput(2995n, currency, i18n.language),
  };
}
