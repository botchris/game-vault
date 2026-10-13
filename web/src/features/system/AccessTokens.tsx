import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { authClient, errorMessage } from '../../api/client';
import { Alert, useFormatters } from '../../components/ui';
import type { AccessToken } from '../../gen/gamevault/v1/auth_pb';
import { copyText } from '../../lib/clipboard';
import { toDate } from '../../lib/model';
import { useAction } from '../../lib/useAction';

/** Access tokens for the browser extension: create one (its secret is shown once), list, revoke. */
export default function AccessTokens() {
  const { t } = useTranslation();
  const fmt = useFormatters();
  const [tokens, setTokens] = useState<AccessToken[]>([]);
  const [name, setName] = useState('');
  const [secret, setSecret] = useState('');
  const [copied, setCopied] = useState(false);
  const { busy, error, setError, run } = useAction();
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
  }, [setError]);

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

  const create = (e: FormEvent) => {
    e.preventDefault();
    dismiss();
    run(async () => {
      const res = await authClient.createAccessToken({ name });
      setSecret(res.secret);
      setAnnounce(t('security.tokens.created'));
      setName('');
      await load();
    });
  };

  const revoke = (tok: AccessToken) => {
    if (!confirm(t('security.tokens.confirmRevoke', { name: tok.name }))) return;
    dismiss();
    run(async () => {
      await authClient.revokeAccessToken({ id: tok.id });
      await load();
    });
  };

  const copy = async () => {
    const ok = await copyText(secret, secretInput.current);
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
