import { useId, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { CopyGrade, CopyRegion, type CopyContent } from '../../gen/gamevault/v1/game_pb';
import { CONTENTS, GRADES, REGIONS, contentKey, gradeKey, regionKey, usedLocations } from '../../lib/model';
import { useAppData } from '../../state/AppData';

// The controls of a physical copy's grade, contents and location, shared by the copy form and the
// scan page's batch bar, so a new grade or content type, or a change to how they read, is made once.

/** The copy's grade, "Not set" first. */
export function GradeSelect({ value, onChange }: { value: CopyGrade; onChange: (grade: CopyGrade) => void }) {
  const { t } = useTranslation();
  return (
    <label>
      {t('copy.grade')}
      <select value={value} onChange={(e) => onChange(Number(e.target.value))}>
        <option value={CopyGrade.UNSPECIFIED}>{t('grade.unspecified')}</option>
        {GRADES.map((g) => <option key={g} value={g}>{t(`grade.${gradeKey(g)}`)}</option>)}
      </select>
    </label>
  );
}

/** The copy's region, "Not set" first. */
export function RegionSelect({ value, onChange }: { value: CopyRegion; onChange: (region: CopyRegion) => void }) {
  const { t } = useTranslation();
  return (
    <label>
      {t('copy.region')}
      <select value={value} onChange={(e) => onChange(Number(e.target.value))}>
        <option value={CopyRegion.UNSPECIFIED}>{t('region.unspecified')}</option>
        {REGIONS.map((r) => <option key={r} value={r}>{t(`region.${regionKey(r)}`)}</option>)}
      </select>
    </label>
  );
}

/** What the box holds, one toggle per content type. */
export function ContentsToggles({ value, onChange, className = '' }: {
  value: CopyContent[];
  onChange: (contents: CopyContent[]) => void;
  className?: string;
}) {
  const { t } = useTranslation();
  return (
    <div className={`${className} field`.trim()}>
      <span className="field-label">{t('copy.contents')}</span>
      <div className="toggles" role="group" aria-label={t('copy.contents')}>
        {CONTENTS.map((c) => {
          const on = value.includes(c);
          return (
            <button type="button" key={c} className={on ? 'toggle on' : 'toggle'} aria-pressed={on}
              onClick={() => onChange(on ? value.filter((x) => x !== c) : [...value, c])}>
              {t(`content.${contentKey(c)}`)}
            </button>
          );
        })}
      </div>
    </div>
  );
}

/** Where the copy is kept, suggesting the places other copies are kept in. */
export function LocationInput({ value, onChange }: { value: string; onChange: (location: string) => void }) {
  const { t } = useTranslation();
  const { games } = useAppData();
  const locations = useMemo(() => usedLocations(games), [games]);
  const listId = `${useId()}-locations`;
  return (
    <label>
      {t('copy.location')}
      <input list={listId} value={value} onChange={(e) => onChange(e.target.value)} placeholder={t('copy.locationPlaceholder')} />
      <datalist id={listId}>{locations.map((l) => <option key={l} value={l} />)}</datalist>
    </label>
  );
}
