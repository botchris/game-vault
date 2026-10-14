import { useId, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { LANGUAGES } from '../i18n';
import { onBackdropPress } from '../lib/backdrop';
import { copyText } from '../lib/clipboard';
import { Icon } from './Icon';
import { useEscape } from './useEscape';

/** Centered dialog. Closes on Escape (when it is the overlay on top) and on backdrop click; the click
 *  never reaches an overlay it was opened over (see lib/backdrop). */
export function Modal(props: { title: ReactNode; onClose: () => void; children: ReactNode; footer?: ReactNode; wide?: boolean; phoneFull?: boolean }) {
  const { onClose } = props;
  const { t } = useTranslation();
  const titleId = useId();
  useEscape(onClose);
  return (
    <div className="overlay" onMouseDown={onBackdropPress(onClose)}>
      <div className={`modal ${props.wide ? 'wide' : ''} ${props.phoneFull ? 'phone-full' : ''}`} role="dialog" aria-modal="true" aria-labelledby={titleId}
        onMouseDown={(e) => e.stopPropagation()}>
        <header className="modal-head">
          <h2 id={titleId}>{props.title}</h2>
          <button className="icon-button" onClick={onClose} aria-label={t('common.close')}><Icon name="close" size={18} /></button>
        </header>
        <div className="modal-body">{props.children}</div>
        {props.footer && <footer className="modal-foot">{props.footer}</footer>}
      </div>
    </div>
  );
}

export function Alert({ tone, children }: { tone: 'error' | 'ok' | 'warn'; children: ReactNode }) {
  return <div className={`alert ${tone}`}>{children}</div>;
}

export function StatCard(props: { label: string; value: number; hint?: string; tone?: 'danger' | 'warn'; active?: boolean; onClick?: () => void }) {
  const { i18n } = useTranslation();
  return (
    <button className={`stat ${props.tone ? `tone-${props.tone}` : ''} ${props.active ? 'active' : ''}`} onClick={props.onClick}>
      <span className="stat-value">{props.value.toLocaleString(i18n.language)}</span>
      <span className="stat-label">{props.label}</span>
      {props.hint && <span className="stat-hint">{props.hint}</span>}
    </button>
  );
}

/** A CD key shown masked, with reveal and copy buttons. Gift links render as links. */
export function KeyCell({ value }: { value: string }) {
  const { t } = useTranslation();
  const [shown, setShown] = useState(false);
  const [copied, setCopied] = useState(false);
  const [failed, setFailed] = useState(false);
  if (!value) return <span className="muted">—</span>;
  if (value.startsWith('http')) {
    return <a href={value} target="_blank" rel="noreferrer">{t('copy.giftLink')}</a>;
  }
  // On plain http over the network the browser hides the Clipboard API and `copyText` falls back
  // to the legacy command; when that fails too, the key is revealed so it can be copied by hand.
  const copy = async () => {
    const ok = await copyText(value);
    setFailed(!ok);
    if (!ok) {
      setShown(true);
      return;
    }
    setCopied(true);
    setTimeout(() => setCopied(false), 1200);
  };
  const toggleLabel = shown ? t('copy.hideKey') : t('copy.showKey');
  const copyLabel = copied ? t('copy.keyCopied') : t('copy.copyKey');
  return (
    <>
      <span className="keycell" onClick={(e) => e.stopPropagation()}>
        <code>{shown ? value : value.replace(/[A-Za-z0-9]/g, '•')}</code>
        <button className="icon" title={toggleLabel} aria-label={toggleLabel} onClick={() => setShown(!shown)}>
          <Icon name={shown ? 'eyeOff' : 'eye'} size={16} />
        </button>
        <button className="icon" title={copyLabel} aria-label={copyLabel} onClick={copy}>
          <Icon name={copied ? 'check' : 'copy'} size={16} />
        </button>
      </span>
      <span className="keycell-status small danger" role="status">{failed ? t('copy.copyFailed') : ''}</span>
    </>
  );
}

export function LanguageSwitcher() {
  const { i18n, t } = useTranslation();
  return (
    <label className="lang">
      <span className="sr-only">{t('nav.language')}</span>
      <select value={i18n.resolvedLanguage} onChange={(e) => i18n.changeLanguage(e.target.value)} aria-label={t('nav.language')}>
        {LANGUAGES.map((l) => <option key={l.code} value={l.code}>{l.name}</option>)}
      </select>
    </label>
  );
}

/** Formats a date (or YYYY-MM-DD string) in the current UI language. */
export function useFormatters() {
  const { i18n } = useTranslation();
  const lang = i18n.language;
  return {
    date: (d: Date | string | null | undefined) => {
      if (!d) return '';
      const date = typeof d === 'string' ? new Date(`${d}T00:00:00`) : d;
      return date.toLocaleDateString(lang, { year: 'numeric', month: 'short', day: 'numeric' });
    },
    dateTime: (d: Date | null | undefined) =>
      d ? d.toLocaleString(lang, { dateStyle: 'medium', timeStyle: 'short' }) : '',
    bytes: (n: bigint | number) => {
      const v = Number(n);
      const units = ['B', 'KB', 'MB', 'GB'];
      const i = Math.min(units.length - 1, Math.floor(Math.log(Math.max(v, 1)) / Math.log(1024)));
      return `${(v / 1024 ** i).toLocaleString(lang, { maximumFractionDigits: 1 })} ${units[i]}`;
    },
  };
}
