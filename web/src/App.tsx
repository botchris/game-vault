import { useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { PageHeader } from './components/PageHeader';
import { Icon, type IconName } from './components/Icon';
import { Alert, LanguageSwitcher } from './components/ui';
import AuthGate, { useAuth } from './features/auth/AuthGate';
import HomePage from './features/home/HomePage';
import LibraryPage from './features/library/LibraryPage';
import LogsPage from './features/logs/LogsPage';
import FieldsPage from './features/fields/FieldsPage';
import ProvidersPage from './features/providers/ProvidersPage';
import ScanPage from './features/scan/ScanPage';
import SourcesPage from './features/sources/SourcesPage';
import SystemPage from './features/system/SystemPage';
import { dotLabel, worstLevel } from './lib/health';
import type { Page } from './lib/route';
import { useHashRoute } from './lib/useHashRoute';
import { HealthProvider, useHealth } from './state/Health';
import { AppDataProvider, useAppData } from './state/AppData';

/** Settings pages, grouped in the sidebar and behind "Settings" on phones. */
const SETTINGS: { route: Page; icon: IconName }[] = [
  { route: 'sources', icon: 'sources' },
  { route: 'providers', icon: 'providers' },
  { route: 'fields', icon: 'fields' },
  { route: 'system', icon: 'system' },
  { route: 'logs', icon: 'logs' },
];

/** The page the address names (#/library, #/scan, #/sources/<id>…) and what to open on it. A new
 *  page starts at the top; an address within the same page (a game's sheet over the library, a
 *  card of System) leaves the scrolling to the page. */
function useRoute() {
  const route = useHashRoute();
  const shown = useRef(route.page);
  useEffect(() => {
    if (shown.current === route.page) return;
    shown.current = route.page;
    window.scrollTo({ top: 0 });
  }, [route.page]);
  return route;
}

export default function App() {
  return (
    <AuthGate>
      <AppDataProvider>
        <HealthProvider>
          <Shell />
        </HealthProvider>
      </AppDataProvider>
    </AuthGate>
  );
}

function Shell() {
  const { t } = useTranslation();
  const { page: route, target } = useRoute();
  const { games, loading, error } = useAppData();
  const { principal, logout } = useAuth();
  const inSettings = SETTINGS.some((s) => s.route === route) || route === 'settings';
  // A dot on System (and on Settings on phones) when something needs attention, colored by the worst.
  const health = worstLevel(useHealth().issues);

  return (
    <div className="shell">
      <aside className="sidebar">
        <a className="brand" href="#/home">Game Vault</a>
        <nav className="side-nav" aria-label={t('nav.main')}>
          <NavLink route="home" icon="home" active={route === 'home'} />
          <NavLink route="library" icon="library" active={route === 'library'} count={games.length} />
          <NavLink route="scan" icon="scan" active={route === 'scan'} />
          <p className="side-group">{t('nav.settings')}</p>
          {SETTINGS.map((s) => <NavLink key={s.route} route={s.route} icon={s.icon} active={route === s.route} dot={s.route === 'system' ? health : null} />)}
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
            {route === 'home' && <HomePage />}
            {route === 'library' && <LibraryPage target={target} />}
            {route === 'scan' && <ScanPage />}
            {route === 'sources' && <SourcesPage target={target} />}
            {route === 'providers' && <ProvidersPage />}
            {route === 'fields' && <FieldsPage />}
            {route === 'system' && <SystemPage target={target} />}
            {route === 'logs' && <LogsPage />}
            {route === 'settings' && <SettingsIndex health={health} />}
          </>
        )}
      </main>

      <nav className="bottom-nav" aria-label={t('nav.main')}>
        <BottomLink route="home" icon="home" active={route === 'home'} />
        <BottomLink route="library" icon="library" active={route === 'library'} />
        <BottomLink route="scan" icon="scan" active={route === 'scan'} />
        <BottomLink route="settings" icon="settings" active={inSettings} label={t('nav.settings')} dot={health} />
      </nav>
    </div>
  );
}

/** A dot saying something needs attention on a page, colored by its level, labelled "System: error". */
function HealthDot({ level, page }: { level: 'error' | 'warning' | null | undefined; page: string }) {
  const { t } = useTranslation();
  if (!level) return null;
  const label = dotLabel(level, page, t);
  return <span className={`health-dot ${level}`} role="img" aria-label={label} title={label} />;
}

function NavLink({ route, icon, active, count, dot }: { route: Page; icon: IconName; active: boolean; count?: number; dot?: 'error' | 'warning' | null }) {
  const { t, i18n } = useTranslation();
  return (
    <a href={`#/${route}`} className={`side-link ${active ? 'active' : ''}`} aria-current={active ? 'page' : undefined}>
      <Icon name={icon} />
      <span>{t(`nav.${route}`)}</span>
      {count !== undefined && <span className="side-count">{count.toLocaleString(i18n.language)}</span>}
      {dot && <span className="side-dot"><HealthDot level={dot} page={t(`nav.${route}`)} /></span>}
    </a>
  );
}

function BottomLink({ route, icon, active, label, dot }: { route: Page; icon: IconName; active: boolean; label?: string; dot?: 'error' | 'warning' | null }) {
  const { t } = useTranslation();
  return (
    <a href={`#/${route}`} className={active ? 'active' : ''} aria-current={active ? 'page' : undefined}>
      <Icon name={icon} size={22} />
      <span>{label ?? t(`nav.${route}`)}</span>
      {dot && <span className="bottom-dot"><HealthDot level={dot} page={label ?? t(`nav.${route}`)} /></span>}
    </a>
  );
}

/** Phone-only list of settings pages (the sidebar shows them on larger screens). */
function SettingsIndex({ health }: { health: 'error' | 'warning' | null }) {
  const { t } = useTranslation();
  const { principal, logout } = useAuth();
  return (
    <div className="page">
      <PageHeader title={t('nav.settings')} />
      <ul className="settings-list">
        {SETTINGS.map((s) => (
          <li key={s.route}>
            <a href={`#/${s.route}`}><Icon name={s.icon} /><span>{t(`nav.${s.route}`)}</span>{s.route === 'system' && <HealthDot level={health} page={t('nav.system')} />}</a>
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
