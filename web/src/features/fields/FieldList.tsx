import { useTranslation } from 'react-i18next';
import { useFormatters } from '../../components/ui';
import type { FieldDefinition } from '../../gen/gamevault/v1/field_pb';
import { fieldText, type Formatters } from '../../lib/fields';
import { formatAmount } from '../../lib/money';
import type { FieldValues } from './FieldInput';

/** The locale pieces fieldText needs, in the UI language. */
export function useFieldFormatters(): Formatters {
  const { t, i18n } = useTranslation();
  const fmt = useFormatters();
  const lang = i18n.language;
  return {
    money: (minor, currency) => formatAmount(minor, currency, lang),
    // A field date may be only a year, or a year and a month.
    date: (d) => {
      if (/^\d{4}$/.test(d)) return d;
      if (/^\d{4}-\d{2}$/.test(d)) return new Date(`${d}-01T00:00:00`).toLocaleDateString(lang, { year: 'numeric', month: 'long' });
      return fmt.date(d);
    },
    yes: t('fields.yes'),
    no: t('fields.no'),
    hours: t('fields.hours'),
    minutes: t('fields.minutes'),
  };
}

/** The custom fields that have a value, as a definition list in the definitions' order; nothing when none has one. */
export function FieldList({ defs, values, className }: { defs: FieldDefinition[]; values: FieldValues; className?: string }) {
  const fmt = useFieldFormatters();
  const rows = defs.map((d) => ({ def: d, text: fieldText(d, values[d.id], fmt) })).filter((r) => r.text);
  if (rows.length === 0) return null;
  return (
    <dl className={`field-list ${className ?? ''}`}>
      {rows.map(({ def, text }) => (
        <div key={def.id} className={def.type === 'longtext' ? 'wide' : undefined}>
          <dt>{def.name}</dt>
          <dd>{text}</dd>
        </div>
      ))}
    </dl>
  );
}
