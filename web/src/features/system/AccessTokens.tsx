import { useCallback, useEffect, useState, type FormEvent } from 'react';
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

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      const res = await authClient.createAccessToken({ name });
      setSecret(res.secret);
      setCopied(false);
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
    try {
      await authClient.revokeAccessToken({ id: tok.id });
      await load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(secret);
      setCopied(true);
    } catch {
      /* no clipboard (plain http on the network): the field selects itself on focus for a manual copy */
    }
  };

  return (
    <>
      <h3>{t('security.tokens.title')}</h3>
      <p className="muted small">{t('security.tokens.intro')}</p>
      {error && <Alert tone="error">{error}</Alert>}
      {secret && (
        <div className="token-secret">
          <Alert tone="warn">{t('security.tokens.created')}</Alert>
          <div className="token-secret-row">
            <input readOnly value={secret} aria-label={t('security.tokens.secret')} spellCheck={false}
              onFocus={(e) => e.currentTarget.select()} />
            <button type="button" onClick={copy}>{copied ? t('security.tokens.copied') : t('security.tokens.copy')}</button>
          </div>
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
