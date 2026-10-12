import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { authClient, errorMessage } from '../../api/client';
import { Alert, useFormatters } from '../../components/ui';
import type { AccessToken } from '../../gen/gamevault/v1/auth_pb';
import { toDate } from '../../lib/model';

/** Access tokens for the browser extension: create one (its secret is shown once), list, revoke. */
export default function AccessTokens() {
  const { t } = useTranslation();
  const fmt = useFormatters();
  const [tokens, setTokens] = useState<AccessToken[]>([]);
  const [name, setName] = useState('');
  const [secret, setSecret] = useState('');
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [hint, setHint] = useState(false);
  const [announce, setAnnounce] = useState('');
  const secretInput = useRef<HTMLInputElement>(null);
  const copiedTimer = useRef<number>(undefined);

  const load = useCallback(async () => {
    try {
      setTokens((await authClient.listAccessTokens({})).tokens);
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => () => window.clearTimeout(copiedTimer.current), []);

  // The secret is shown once: leaving it in state would keep it in memory and on screen for no reason.
  const dismiss = () => {
    setSecret('');
    setCopied(false);
    setHint(false);
    setAnnounce('');
  };

  // After creating a token the secret field takes the focus (an explicit result of the user's action), so a
  // manual copy is one gesture away; it is announced because the field appears out of the user's reading order.
  useEffect(() => {
    if (!secret) return;
    secretInput.current?.focus();
    secretInput.current?.select();
  }, [secret]);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    dismiss();
    try {
      const res = await authClient.createAccessToken({ name });
      setSecret(res.secret);
      setAnnounce(t('security.tokens.created'));
      setName('');
      await load();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const revoke = async (tok: AccessToken) => {
    if (!confirm(t('security.tokens.confirmRevoke', { name: tok.name }))) return;
    setError('');
    dismiss();
    try {
      await authClient.revokeAccessToken({ id: tok.id });
      await load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const copy = async () => {
    let ok = false;
    try {
      await navigator.clipboard.writeText(secret);
      ok = true;
    } catch {
      // No clipboard API (plain http on the network): select the field and try the legacy command.
      secretInput.current?.focus();
      secretInput.current?.select();
      try {
        ok = document.execCommand('copy');
      } catch {
        ok = false;
      }
    }
    setCopied(ok);
    setHint(!ok);
    setAnnounce(ok ? t('security.tokens.copied') : t('security.tokens.copyManually'));
    window.clearTimeout(copiedTimer.current);
    copiedTimer.current = window.setTimeout(() => setCopied(false), 2500);
  };

  return (
    <>
      <h3>{t('security.tokens.title')}</h3>
      <p className="muted small">{t('security.tokens.intro')}</p>
      <div className="sr-only" role="status" aria-live="polite">{announce}</div>
      {error && <Alert tone="error">{error}</Alert>}
      {secret && (
        <div className="token-secret">
          <Alert tone="warn">{t('security.tokens.created')}</Alert>
          <div className="token-secret-row">
            <input ref={secretInput} readOnly value={secret} aria-label={t('security.tokens.secret')} spellCheck={false}
              onFocus={(e) => e.currentTarget.select()} />
            <button type="button" onClick={copy}>{copied ? t('security.tokens.copied') : t('security.tokens.copy')}</button>
            <button type="button" onClick={dismiss}>{t('security.tokens.done')}</button>
          </div>
          {hint && <p className="muted small">{t('security.tokens.copyManually')}</p>}
        </div>
      )}
      {tokens.length === 0 ? <p className="muted small">{t('security.tokens.none')}</p> : (
        <ul className="token-list">
          {tokens.map((tok) => {
            const used = toDate(tok.lastUsedAt);
            return (
              <li key={tok.id}>
                <span className="token-name">{tok.name}</span>
                <span className="muted small">
                  {t('security.tokens.createdAt', { date: fmt.dateTime(toDate(tok.createdAt)) })}
                  {' · '}
                  {used ? t('security.tokens.lastUsed', { date: fmt.dateTime(used) }) : t('security.tokens.neverUsed')}
                </span>
                <button type="button" className="danger" onClick={() => revoke(tok)}>{t('security.tokens.revoke')}</button>
              </li>
            );
          })}
        </ul>
      )}
      <form className="token-form" onSubmit={create}>
        <label>
          {t('security.tokens.name')}
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('security.tokens.namePlaceholder')}
            maxLength={60} required />
        </label>
        <button type="submit" className="primary" disabled={busy || !name.trim()}>{t('security.tokens.create')}</button>
      </form>
    </>
  );
}
