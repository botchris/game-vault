import { useTranslation } from 'react-i18next';
import { Icon } from '../../components/Icon';
import { PlatformBadge } from '../../components/PlatformBadge';
import { KeyCell, useFormatters } from '../../components/ui';
import { deadlineTone } from '../../lib/deadlines';
import { formatAmount } from '../../lib/money';
import { CopyKind, daysUntil, describePhysical, kindKey, redeemUrl, statusKey, type Copy, type Game } from '../../lib/model';
import { useAppData } from '../../state/AppData';
import { FieldList } from '../fields/FieldList';
import CopyPhotos from './CopyPhotos';
import CopyValue from './CopyValue';
import type { SheetActions } from './sheet';

/** One copy in the Copies tab: kind, platform, status, key, details, value, fields, photos. */
export default function CopyCard({ game, copy, actions, redeeming, onRedeeming, onMarkRedeemed, onDelete, onShowEdition }: {
  game: Game;
  copy: Copy;
  actions: SheetActions;
  /** The copy's key was just opened in the store: offer to mark it as redeemed. */
  redeeming: boolean;
  onRedeeming: () => void;
  onMarkRedeemed: () => void;
  onDelete: () => void;
  onShowEdition: (system: string) => void;
}) {
  const { t, i18n } = useTranslation();
  const fmt = useFormatters();
  const { sourceName, fields } = useAppData();
  const { busy, openDialog } = actions;
  const copyDefs = fields.filter((f) => f.scope === 'copy');
  const d = copy.details!;
  const days = daysUntil(d.redeemBy);
  const source = copy.sourceId ? sourceName(copy.sourceId) : '';
  const { grade, contents } = describePhysical(t, d);
  const physical = [
    grade,
    contents,
    d.price?.amountMinor ? formatAmount(d.price.amountMinor, d.price.currency, i18n.language) : '',
    d.location,
  ];
  const extra = [d.origin, d.edition, ...physical, fmt.date(d.acquiredOn), d.barcode && `EAN ${d.barcode}`].filter(Boolean);
  return (
    <li className={`copy-card kind-edge-${kindKey(d.kind)}`}>
      <div className="copy-top">
        <span className={`kind kind-${kindKey(d.kind)}`}>{t(`kind.${kindKey(d.kind)}`)}</span>
        {d.platform && <PlatformBadge platform={d.platform} full />}
        <span className={`status status-${statusKey(d.status)}`}>{t(`status.${statusKey(d.status)}`)}</span>
        {copy.redundant && <span className="badge warn" title={t('copy.redundantHelp')}>{t('copy.redundant')}</span>}
        <span className="spacer" />
        <span className="copy-actions">
          <button className="icon-button" title={t('common.edit')} aria-label={t('common.edit')} onClick={() => openDialog({ type: 'editCopy', copy })}><Icon name="edit" size={18} /></button>
          <button className="icon-button" title={t('copy.move')} aria-label={t('copy.move')} onClick={() => openDialog({ type: 'moveCopy', copy })}><Icon name="move" size={18} /></button>
          <button className="icon-button" title={t('common.delete')} aria-label={t('common.delete')} onClick={() => onDelete()}><Icon name="trash" size={18} /></button>
        </span>
      </div>
      {d.kind === CopyKind.KEY && (
        <div className="copy-key">
          <KeyCell value={d.key} />
          {redeemUrl(copy) && (
            <a className="button small-button" href={redeemUrl(copy)!} target="_blank" rel="noreferrer" onClick={() => onRedeeming()}>
              {t('copy.redeemOn', { platform: d.platform })}<Icon name="external" size={14} />
            </a>
          )}
          {redeeming && (
            <button className="small-button primary" onClick={() => onMarkRedeemed()} disabled={busy}><Icon name="check" size={14} />{t('copy.markRedeemed')}</button>
          )}
        </div>
      )}
      {days !== null && (
        <p className={`copy-deadline ${deadlineTone(days) ?? 'muted'}`}>
          {t('copy.redeemBy')}: {fmt.date(d.redeemBy)} · {t('common.daysLeft', { count: days })}
        </p>
      )}
      {extra.length > 0 && <p className="muted small">{extra.join(' · ')}</p>}
      {d.kind === CopyKind.PHYSICAL && <CopyValue game={game} copy={copy} onAddBarcode={() => openDialog({ type: 'editCopy', copy })} />}
      <FieldList className="copy-fields" values={d.fields}
        defs={copyDefs.filter((f) => !f.kinds.length || f.kinds.includes(d.kind))} />
      {d.notes && <p className="small">{d.notes}</p>}
      <CopyPhotos game={game} copy={copy} onEditionCover={onShowEdition} />
      {source && <p className="muted small">{t('copy.syncedFrom', { source })}</p>}
    </li>
  );
}
