import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../../components/PageHeader';
import { useAppData } from '../../state/AppData';
import { useHealth } from '../../state/Health';
import { CollectionBlock } from './CollectionBlock';
import { homeEntrance, homeEntranceTaken } from './entrance';
import { PendingStrip } from './PendingStrip';

/** The start page: what needs attention, the external blocks (deals, releases) and the collection
 *  at a glance. Pending and collection come from what the app already holds; nothing here asks an
 *  external service when the page opens. */
export default function HomePage() {
  const { t } = useTranslation();
  const { games } = useAppData();
  const { issues } = useHealth();
  // Decided on the first render and marked after it, so strict mode's second render keeps the
  // entrance; it fades only when the app opened on Home (see entrance.ts).
  const [entering] = useState(homeEntrance);
  useEffect(homeEntranceTaken, []);

  return (
    <div className={entering ? 'page home home-enter' : 'page home'}>
      <PageHeader title={t('home.title')} />
      <PendingStrip games={games} issues={issues} />
      <CollectionBlock games={games} />
    </div>
  );
}
