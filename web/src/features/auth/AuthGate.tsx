import { createContext, useCallback, useContext, useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { authClient, errorMessage, UNAUTHENTICATED_EVENT } from '../../api/client';
import { Alert, LanguageSwitcher } from '../../components/ui';
import type { GetAuthStatusResponse, Principal } from '../../gen/gamevault/v1/auth_pb';

interface AuthCtx {
  principal: Principal;
  /** The request comes straight from a trusted network. */
  trusted: boolean;
  logout: () => Promise<void>;
  /** Reload who the caller is (after creating the user or changing security settings). */
  refresh: () => Promise<void>;
}

const Ctx = createContext<AuthCtx | null>(null);

export function useAuth(): AuthCtx {
  const v = useContext(Ctx);
  if (!v) throw new Error('useAuth must be used inside AuthGate');
  return v;
}

/** Shows the app only to authenticated callers; otherwise the sign-in or first-time setup screen. */
export default function AuthGate({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const [status, setStatus] = useState<GetAuthStatusResponse | null>(null);
  const [error, setError] = useState('');

  const refresh = useCallback(async () => {
    try {
      setStatus(await authClient.getAuthStatus({}));
      setError('');
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    refresh();
    window.addEventListener(UNAUTHENTICATED_EVENT, refresh);
    return () => window.removeEventListener(UNAUTHENTICATED_EVENT, refresh);
  }, [refresh]);

  const logout = useCallback(async () => {
    await authClient.logout({});
    await refresh();
  }, [refresh]);

  if (error) return <Screen><Alert tone="error">{t('app.backendError', { error })}</Alert></Screen>;
  if (!status) return <Screen><p className="muted">{t('common.loading')}</p></Screen>;
  if (status.principal) {
    return <Ctx.Provider value={{ principal: status.principal, trusted: status.trusted, logout, refresh }}>{children}</Ctx.Provider>;
  }
  if (status.canSetup) return <Screen><CredentialsForm mode="setup" onDone={refresh} /></Screen>;
  return (
    <Screen>
      {status.setupRequired ? (
        <div className="card">
          <h2>{t('auth.notAllowed.title')}</h2>
          <p>{t('auth.notAllowed.noUser')}</p>
          <button onClick={refresh}>{t('auth.retry')}</button>
        </div>
      ) : (
        <CredentialsForm mode="login" onDone={refresh} />
      )}
    </Screen>
  );
}

function Screen({ children }: { children: ReactNode }) {
  return (
    <div className="auth-screen">
      <header className="auth-head">
        <h1>🎮 Game Vault</h1>
        <LanguageSwitcher />
      </header>
      {children}
    </div>
  );
}

function CredentialsForm({ mode, onDone }: { mode: 'login' | 'setup'; onDone: () => void }) {
  const { t } = useTranslation();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      if (mode === 'setup') await authClient.setup({ username, password });
      else await authClient.login({ username, password });
      onDone();
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  };

  return (
    <form className="card auth-form" onSubmit={submit}>
      <h2>{t(`auth.${mode}.title`)}</h2>
      <p className="muted">{t(`auth.${mode}.intro`)}</p>
      <label>
        {t('auth.username')}
        <input autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required />
      </label>
      <label>
        {t('auth.password')}
        <input type="password" autoComplete={mode === 'setup' ? 'new-password' : 'current-password'} value={password}
          onChange={(e) => setPassword(e.target.value)} required minLength={mode === 'setup' ? 8 : undefined} />
      </label>
      {error && <Alert tone="error">{error}</Alert>}
      <button type="submit" className="primary" disabled={busy}>{busy ? t('common.working') : t(`auth.${mode}.submit`)}</button>
    </form>
  );
}
