import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../../components/PageHeader';
import { useAppData } from '../../state/AppData';
import { useHealth } from '../../state/Health';
import { CollectionBlock } from './CollectionBlock';
import { PendingStrip } from './PendingStrip';

// Whether Home has been shown in this session: it fades in only the first time, when the app opens
// on it, and never when the user comes back to it from another page (page switches do not animate).
let shownBefore = false;

/** The start page: what needs attention, the external blocks (deals, releases) and the collection
 *  at a glance. Pending and collection come from what the app already holds; nothing here asks an
 *  external service when the page opens. */
export default function HomePage() {
  const { t } = useTranslation();
  const { games } = useAppData();
  const { issues } = useHealth();
  // Decided on the first render and marked after it, so strict mode's second render keeps the entrance.
  const [entering] = useState(() => !shownBefore);
  useEffect(() => { shownBefore = true; }, []);

  return (
    <div className={entering ? 'page home home-enter' : 'page home'}>
      <PageHeader title={t('home.title')} />
      <PendingStrip games={games} issues={issues} />
      <CollectionBlock games={games} />
    </div>
  );
}
