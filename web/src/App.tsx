import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Icon, type IconName } from './components/Icon';
import { Alert, LanguageSwitcher } from './components/ui';
import AuthGate, { useAuth } from './features/auth/AuthGate';
import LibraryPage from './features/library/LibraryPage';
import LogsPage from './features/logs/LogsPage';
import FieldsPage from './features/fields/FieldsPage';
import ProvidersPage from './features/providers/ProvidersPage';
import ScanPage from './features/scan/ScanPage';
import SourcesPage from './features/sources/SourcesPage';
import SystemPage from './features/system/SystemPage';
import { AppDataProvider, useAppData } from './state/AppData';

const ROUTES = ['library', 'scan', 'sources', 'providers', 'fields', 'system', 'logs', 'settings'] as const;
type Route = (typeof ROUTES)[number];

/** Settings pages, grouped in the sidebar and behind "Settings" on phones. */
const SETTINGS: { route: Route; icon: IconName }[] = [
  { route: 'sources', icon: 'sources' },
  { route: 'providers', icon: 'providers' },
  { route: 'fields', icon: 'fields' },
  { route: 'system', icon: 'system' },
  { route: 'logs', icon: 'logs' },
];

/** Tiny hash router: #/library, #/scan, #/sources… */
function useRoute(): Route {
  const read = (): Route => {
    const r = window.location.hash.replace(/^#\/?/, '') as Route;
    return ROUTES.includes(r) ? r : 'library';
  };
  const [route, setRoute] = useState(read);
  useEffect(() => {
    const onChange = () => {
      setRoute(read());
      window.scrollTo({ top: 0 });
    };
    window.addEventListener('hashchange', onChange);
    return () => window.removeEventListener('hashchange', onChange);
  }, []);
  return route;
}

export default function App() {
  return (
    <AuthGate>
      <AppDataProvider>
        <Shell />
      </AppDataProvider>
    </AuthGate>
  );
}

function Shell() {
  const { t } = useTranslation();
  const route = useRoute();
  const { games, loading, error } = useAppData();
  const { principal, logout } = useAuth();
  const inSettings = SETTINGS.some((s) => s.route === route) || route === 'settings';

  return (
    <div className="shell">
      <aside className="sidebar">
        <a className="brand" href="#/library">Game Vault</a>
        <nav className="side-nav" aria-label={t('nav.main')}>
          <NavLink route="library" icon="library" active={route === 'library'} count={games.length} />
          <NavLink route="scan" icon="scan" active={route === 'scan'} />
          <p className="side-group">{t('nav.settings')}</p>
          {SETTINGS.map((s) => <NavLink key={s.route} route={s.route} icon={s.icon} active={route === s.route} />)}
        </nav>
        <div className="side-foot">
          {principal.method === 'session' && (
            <div className="whoami">
              <span title={t(`security.methods.${principal.method}`)}>{principal.name}</span>
              {principal.method === 'session' && (
                <button className="icon-button" onClick={logout} title={t('auth.logout')} aria-label={t('auth.logout')}><Icon name="logout" size={18} /></button>
              )}
            </div>
          )}
          <LanguageSwitcher />
        </div>
      </aside>

      <main className="main">
        {error && <Alert tone="error">{t('app.backendError', { error })}</Alert>}
        {loading ? <p className="muted page-loading">{t('common.loading')}</p> : (
          <>
            {route === 'library' && <LibraryPage />}
            {route === 'scan' && <ScanPage />}
            {route === 'sources' && <SourcesPage />}
            {route === 'providers' && <ProvidersPage />}
            {route === 'fields' && <FieldsPage />}
            {route === 'system' && <SystemPage />}
            {route === 'logs' && <LogsPage />}
            {route === 'settings' && <SettingsIndex />}
          </>
        )}
      </main>

      <nav className="bottom-nav" aria-label={t('nav.main')}>
        <BottomLink route="library" icon="library" active={route === 'library'} />
        <BottomLink route="scan" icon="scan" active={route === 'scan'} />
        <BottomLink route="settings" icon="settings" active={inSettings} label={t('nav.settings')} />
      </nav>
    </div>
  );
}

function NavLink({ route, icon, active, count }: { route: Route; icon: IconName; active: boolean; count?: number }) {
  const { t, i18n } = useTranslation();
  return (
    <a href={`#/${route}`} className={`side-link ${active ? 'active' : ''}`} aria-current={active ? 'page' : undefined}>
      <Icon name={icon} />
      <span>{t(`nav.${route}`)}</span>
      {count !== undefined && <span className="side-count">{count.toLocaleString(i18n.language)}</span>}
    </a>
  );
}

function BottomLink({ route, icon, active, label }: { route: Route; icon: IconName; active: boolean; label?: string }) {
  const { t } = useTranslation();
  return (
    <a href={`#/${route}`} className={active ? 'active' : ''} aria-current={active ? 'page' : undefined}>
      <Icon name={icon} size={22} />
      <span>{label ?? t(`nav.${route}`)}</span>
    </a>
  );
}

/** Phone-only list of settings pages (the sidebar shows them on larger screens). */
function SettingsIndex() {
  const { t } = useTranslation();
  const { principal, logout } = useAuth();
  return (
    <div className="page">
      <h1 className="page-title">{t('nav.settings')}</h1>
      <ul className="settings-list">
        {SETTINGS.map((s) => (
          <li key={s.route}>
            <a href={`#/${s.route}`}><Icon name={s.icon} /><span>{t(`nav.${s.route}`)}</span></a>
          </li>
        ))}
      </ul>
      <div className="settings-foot">
        <LanguageSwitcher />
        {principal.method === 'session' && <button onClick={logout}><Icon name="logout" size={18} /> {t('auth.logout')}</button>}
      </div>
    </div>
  );
}
