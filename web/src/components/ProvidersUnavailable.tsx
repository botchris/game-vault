import { useTranslation } from 'react-i18next';

/**
 * Says that some providers had a provider-level problem (a used-up allowance, a bot check, rejected
 * settings), so an answer may be incomplete, and links to System → Health, which says which and why.
 */
export default function ProvidersUnavailable() {
  const { t } = useTranslation();
  return <span className="muted small">{t('common.providersUnavailable')} <a href="#/system">{t('common.seeHealth')}</a></span>;
}
