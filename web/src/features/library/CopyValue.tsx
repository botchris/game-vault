import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, valuationClient } from '../../api/client';
import { useFormatters } from '../../components/ui';
import type { Estimate, Money } from '../../gen/gamevault/v1/game_pb';
import { formatAmount } from '../../lib/money';
import { toDate, type Copy, type Game } from '../../lib/model';
import { usePriceProviders } from '../../lib/usePriceProviders';
import { useAppData } from '../../state/AppData';

/** A physical copy's second-hand prices: one line per source, the date, and "Update price". */
export default function CopyValue({ game, copy, onAddBarcode }: { game: Game; copy: Copy; onAddBarcode: () => void }) {
  const { t, i18n } = useTranslation();
  const fmt = useFormatters();
  const { putGame } = useAppData();
  const providers = usePriceProviders();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  if (providers.size === 0) return null;
  if (!copy.details?.barcode) {
    return <p className="small copy-value"><button className="link" onClick={onAddBarcode}>{t('value.addBarcode')}</button></p>;
  }

  const money = (m?: Money) => (m?.amountMinor ? formatAmount(m.amountMinor, m.currency, i18n.language) : '');
  const line = (e: Estimate) => {
    const name = providers.get(e.provider) ?? e.provider;
    if (e.listings > 0) return t('value.listed', { name, price: money(e.sell), count: e.listings });
    const sells = t('value.sells', { name, price: money(e.sell) });
    return e.buyCash?.amountMinor || e.buyCredit?.amountMinor ? sells + t('value.pays', { cash: money(e.buyCash), credit: money(e.buyCredit) }) : sells;
  };
  const update = async () => {
    setBusy(true);
    setError('');
    try {
      const res = await valuationClient.estimateCopy({ gameId: game.id, copyId: copy.id });
      putGame(res.game!);
      if (res.warnings.length) setError(res.warnings.join(' · '));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  // Estimates of a provider disabled since are left out, as they are of the totals.
  const estimates = copy.estimates.filter((e) => providers.has(e.provider));
  const fetched = estimates.map((e) => toDate(e.fetchedAt)).filter((d): d is Date => !!d);
  const latest = fetched.length ? new Date(Math.max(...fetched.map((d) => d.getTime()))) : null;
  const planned = toDate(copy.nextValuation);
  const checked = toDate(copy.valuedAt);
  const status = latest ? fmt.date(latest)
    : checked ? t('value.notListed', { date: fmt.date(checked) })
      : planned ? t('value.planned', { date: fmt.date(planned) }) : t('value.pending');

  return (
    <div className="copy-value small">
      {estimates.map((e) => (
        <a key={e.provider} href={e.url || undefined} target="_blank" rel="noreferrer">{line(e)}</a>
      ))}
      <span className="muted">{status}</span>
      <button className="link" onClick={update} disabled={busy}>{busy ? t('value.updating') : t('value.update')}</button>
      {error && <span className="copy-value-error">{error}</span>}
    </div>
  );
}
