import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { Game } from '../../gen/gamevault/v1/game_pb';
import type { HealthIssue } from '../../gen/gamevault/v1/health_pb';
import { pendingItems, type PendingItem } from '../../lib/home';

/** What needs the user's attention, as chips that open the library view (or System) behind each:
 *  the counts use the library's own rules, so a chip says what its view shows. */
export function PendingStrip({ games, issues }: { games: Game[]; issues: HealthIssue[] }) {
  const { t } = useTranslation();
  const items = useMemo(() => pendingItems(games, issues, new Date()), [games, issues]);

  if (!items.length) return <p className="muted home-caught-up">{t('home.pending.none')}</p>;

  const when = (days: number) => (days < 0 ? t('home.pending.expired') : days === 0 ? t('home.pending.today') : t('home.pending.inDays', { count: days }));
  const label = (it: PendingItem) => {
    switch (it.kind) {
      case 'expiring':
        return t('home.pending.expiring', { count: it.count, title: it.nearest.title, when: when(it.nearest.days) });
      case 'health':
        return t('home.pending.health', { count: it.count });
      default:
        return t(`home.pending.${it.kind}`, { count: it.count });
    }
  };
  const tone = (it: PendingItem) => (it.kind === 'expiring' || (it.kind === 'health' && it.level === 'error') ? 'danger' : it.kind === 'health' ? 'warn' : '');

  return (
    <nav className="home-pending" aria-label={t('home.pending.label')}>
      {items.map((it) => <a key={it.kind} className={`home-chip ${tone(it)}`} href={it.href}>{label(it)}</a>)}
    </nav>
  );
}
