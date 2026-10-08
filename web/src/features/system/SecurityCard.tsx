import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { authClient, errorMessage } from '../../api/client';
import { Alert } from '../../components/ui';
import type { GetAuthSettingsResponse } from '../../gen/gamevault/v1/auth_pb';
import { useAuth } from '../auth/AuthGate';

const AUTH_MODES = ['trusted_networks', 'required'] as const;
const CERT_MODES = ['enabled', 'local_disabled', 'disabled'] as const;

/** Security settings: when a password is needed, trusted networks, the user, certificate validation. */
export default function SecurityCard() {
  const { t } = useTranslation();
  const { principal, refresh } = useAuth();
  const [data, setData] = useState<GetAuthSettingsResponse | null>(null);
  const [authentication, setAuthentication] = useState('trusted_networks');
  const [networks, setNetworks] = useState('');
  const [certs, setCerts] = useState('enabled');
  const [notice, setNotice] = useState<{ tone: 'ok' | 'error'; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await authClient.getAuthSettings({});
      setData(res);
      setAuthentication(res.settings!.authentication);
      setNetworks(res.settings!.trustedNetworks.join('\n'));
      setCerts(res.settings!.certificateValidation);
    } catch (e) {
      setNotice({ tone: 'error', text: errorMessage(e) });
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  if (!data) return null;
  const s = data.settings!;
  const typedNetworks = networks.split(/[\n,]/).map((n) => n.trim()).filter(Boolean);
  const dirty = authentication !== s.authentication || certs !== s.certificateValidation ||
    typedNetworks.join('\n') !== s.trustedNetworks.join('\n');

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setNotice(null);
    try {
      await authClient.updateAuthSettings({ settings: { authentication, trustedNetworks: typedNetworks, certificateValidation: certs } });
      await load();
      await refresh();
      setNotice({ tone: 'ok', text: t('security.saved') });
    } catch (err) {
      setNotice({ tone: 'error', text: errorMessage(err) });
    } finally {
      setBusy(false);
    }
  };

  const addMine = () => setNetworks((n) => [n.trim(), data.clientAddress].filter(Boolean).join('\n'));

  return (
    <section className="card">
      <h2>{t('security.title')}</h2>
      <p className="muted small">{principal.method === 'trusted' ? t('security.youAreTrusted') : t('security.youAre', { name: principal.name })}</p>
      {notice && <Alert tone={notice.tone}>{notice.text}</Alert>}

      <form className="grid security-form" onSubmit={save}>
        <div className="span2 field">
          <label htmlFor="sec-auth">{t('security.authentication.label')}</label>
          <select id="sec-auth" value={authentication} onChange={(e) => setAuthentication(e.target.value)}>
            {AUTH_MODES.map((m) => <option key={m} value={m}>{t(`security.authentication.${m}`)}</option>)}
          </select>
          <span className="help">{t(`security.authentication.${authentication}Help`)}</span>
        </div>

        <div className="span2 field">
          <label htmlFor="sec-networks">{t('security.networks.label')}</label>
          <textarea id="sec-networks" rows={3} value={networks} onChange={(e) => setNetworks(e.target.value)}
            disabled={authentication === 'required'} placeholder="127.0.0.0/8&#10;192.168.1.0/24" spellCheck={false} />
          <span className="help">
            {t('security.networks.help')}{' '}
            {data.clientAddress && (
              <>
                {t('security.networks.youAreAt', { address: data.clientAddress })}{' '}
                {!data.clientInTrustedNetworks && !typedNetworks.includes(data.clientAddress) && authentication !== 'required' && (
                  <button type="button" className="link" onClick={addMine}>{t('security.networks.addMine')}</button>
                )}
              </>
            )}
          </span>
        </div>

        <div className="span2 field">
          <label htmlFor="sec-certs">{t('security.certificates.label')}</label>
          <select id="sec-certs" value={certs} onChange={(e) => setCerts(e.target.value)}>
            {CERT_MODES.map((m) => <option key={m} value={m}>{t(`security.certificates.${m}`)}</option>)}
          </select>
          <span className="help">{t('security.certificates.help')}</span>
        </div>

        <div className="span2 actions">
          <span className="spacer" />
          {dirty && <button type="button" onClick={load} disabled={busy}>{t('common.discard')}</button>}
          <button type="submit" className="primary" disabled={busy || !dirty}>{t('common.save')}</button>
        </div>
      </form>

      <h3>{t('security.user.title')}</h3>
      <UserForm hasUser={data.hasUser} username={data.username} signedIn={principal.method === 'session'} onDone={async () => { await load(); await refresh(); }} />
    </section>
  );
}

/** Creates the user, or changes its name and password. */
function UserForm({ hasUser, username: initial, signedIn, onDone }: {
  hasUser: boolean; username: string; signedIn: boolean; onDone: () => void;
}) {
  const { t } = useTranslation();
  const [username, setUsername] = useState(initial);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [result, setResult] = useState<{ tone: 'ok' | 'error'; text: string } | null>(null);

  useEffect(() => setUsername(initial), [initial]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setResult(null);
    try {
      if (!hasUser) await authClient.setup({ username, password: next });
      else await authClient.changePassword({ username, currentPassword: current, newPassword: next });
      setResult({ tone: 'ok', text: t(hasUser ? 'security.user.changed' : 'security.user.created') });
      setCurrent('');
      setNext('');
      onDone();
    } catch (err) {
      setResult({ tone: 'error', text: errorMessage(err) });
    }
  };

  return (
    <form className="grid grid-3" onSubmit={submit}>
      <p className="muted small span-all">{t(hasUser ? 'security.user.changeIntro' : 'security.user.createIntro')}</p>
      <label>
        {t('auth.username')}
        <input autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required minLength={3} />
      </label>
      {hasUser && signedIn && (
        <label>
          {t('security.user.current')}
          <input type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
        </label>
      )}
      <label>
        {t(hasUser ? 'security.user.new' : 'auth.password')}
        <input type="password" autoComplete="new-password" minLength={8} value={next} onChange={(e) => setNext(e.target.value)} required />
      </label>
      {result && <div className="span-all"><Alert tone={result.tone}>{result.text}</Alert></div>}
      <div className="actions span-all">
        <span className="spacer" />
        <button type="submit" className="primary">{t(hasUser ? 'security.user.change' : 'security.user.create')}</button>
      </div>
    </form>
  );
}
