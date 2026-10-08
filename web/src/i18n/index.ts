import i18n from 'i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import { initReactI18next } from 'react-i18next';
import en from './locales/en.json';
import es from './locales/es.json';

/** Available UI languages. To add one: create locales/<code>.json and register it here. */
export const LANGUAGES = [
  { code: 'en', name: 'English' },
  { code: 'es', name: 'Español' },
] as const;

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: { en: { translation: en }, es: { translation: es } },
    fallbackLng: 'en',
    supportedLngs: LANGUAGES.map((l) => l.code),
    nonExplicitSupportedLngs: true, // "es-ES" → "es"
    interpolation: { escapeValue: false }, // React already escapes
    detection: { order: ['localStorage', 'navigator'], caches: ['localStorage'], lookupLocalStorage: 'gamevault.lang' },
  });

i18n.on('languageChanged', (lng) => {
  document.documentElement.lang = lng;
});

export default i18n;
