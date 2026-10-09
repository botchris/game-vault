import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Icon } from './Icon';

/** `code` spans inside help text. */
function inline(text: string): ReactNode[] {
  return text.split(/(`[^`]+`)/g).map((part, i) =>
    part.startsWith('`') && part.endsWith('`') ? <code key={i}>{part.slice(1, -1)}</code> : part);
}

/**
 * Help under a settings field. The text is plain lines: lines starting with "1." / "2)" become a
 * numbered list of steps, the others paragraphs. The help link is shown as a button above them.
 */
export function FieldHelp({ text, url, linkLabel }: { text: string; url?: string; linkLabel?: string }) {
  const { t } = useTranslation();
  const blocks: ReactNode[] = [];
  let steps: string[] = [];
  const flush = () => {
    if (steps.length) {
      blocks.push(<ol key={`ol${blocks.length}`} className="help-steps">{steps.map((s, i) => <li key={i}>{inline(s)}</li>)}</ol>);
      steps = [];
    }
  };
  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (!line) continue;
    const step = line.match(/^\d+[.)]\s+(.*)$/);
    if (step) {
      steps.push(step[1]);
      continue;
    }
    flush();
    blocks.push(<p key={`p${blocks.length}`}>{inline(line)}</p>);
  }
  flush();
  return (
    <span className="field-help">
      {url && (
        <a className="button small-button help-link" href={url} target="_blank" rel="noreferrer">
          {linkLabel ?? t('sources.openLink')}<Icon name="external" size={14} />
        </a>
      )}
      {blocks}
    </span>
  );
}
