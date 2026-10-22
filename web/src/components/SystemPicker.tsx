import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode, type RefObject } from 'react';
import { isPhone, usePhone } from '../lib/usePhone';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { systemCounts } from '../lib/editions';
import { pickerGroups, systemKey, type PickerGroup } from '../lib/systems';
import { useAppData } from '../state/AppData';
import { useEscape } from './useEscape';

// A text field for a game system: free text is allowed, and a list of the catalog's systems
// (the user's own first, then by maker) helps to type a name the server knows. On desktop the
// list is a popover under the field; on phones the field is read-only and opens a bottom sheet
// with its own search field, so the keyboard comes up only when the user wants to search.
//
// Both overlays are manual popovers (the browser's top layer) rendered into <body>: the field
// usually sits in a dialog whose body scrolls and would clip a positioned list, and a portal keeps
// the list out of the <label>, whose activation would reopen the list on every pick.

/** One choosable line of the list: a catalog system, or the typed text kept as it is. */
interface Choice {
  value: string;
  label: string;
}

/** The choices in list order (the groups', then "Use …"), for the keyboard. */
function flatten(groups: PickerGroup[], use: Choice | null): Choice[] {
  const all = groups.flatMap((g) => g.options.map((o) => ({ value: o.name, label: o.alias ? `${o.name} · ${o.alias}` : o.name })));
  return use ? [...all, use] : all;
}

/** The groups for `query` and the "Use “…”" choice when the text is not exactly a catalog name. */
function useChoices(query: string) {
  const { t } = useTranslation();
  const { systems, games } = useAppData();
  const owned = useMemo(() => systemCounts(games).map(([s]) => s).filter(Boolean), [games]);
  return useMemo(() => {
    const groups = pickerGroups(systems, owned, query);
    const text = query.trim();
    const exact = text !== '' && systems.some((s) => systemKey(s.name) === systemKey(text));
    const use = text && !exact ? { value: text, label: t('systemPicker.use', { text }) } : null;
    return { groups, use, choices: flatten(groups, use) };
  }, [systems, owned, query, t]);
}

interface Props {
  value: string;
  onChange: (value: string) => void;
  /** The visible label text; the component renders the <label>. */
  label: string;
  placeholder?: string;
  /** Focuses the field on mount. It never opens the list: only the user does. */
  autoFocus?: boolean;
  className?: string;
  /** Shown under the field, inside the label (help text). */
  children?: ReactNode;
}

/** A searchable system field: a popover list on desktop, a bottom sheet on phones. */
export default function SystemPicker({ value, onChange, label, placeholder, autoFocus, className, children }: Props) {
  const phone = usePhone();
  const inputRef = useRef<HTMLInputElement>(null);
  const listId = useId();
  // null: closed. A string: open, listing the systems that match it ('' lists them all).
  const [query, setQuery] = useState<string | null>(null);
  // The phone sheet is chosen when the list opens, so a rotation does not swap it under the user.
  const [sheet, setSheet] = useState(false);
  const [active, setActive] = useState(-1);
  const open = query !== null;
  const { groups, use, choices } = useChoices(query ?? '');

  const openList = (q: string, at = -1) => {
    setSheet(isPhone());
    setQuery(q);
    setActive(at);
  };
  // Closing the sheet hands the focus back to its field (the popover never took it).
  const close = () => {
    setQuery(null);
    setActive(-1);
    if (sheet) inputRef.current?.focus();
  };
  const pick = (v: string) => {
    onChange(v);
    close();
  };

  // Opening without typing lists everything, with the current system (if listed) as the active one.
  // While closed the choices are already those of an empty query.
  const openAll = () => openList('', choices.findIndex((c) => c.value === value));

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    // Keys that confirm an input method's composition (Enter in Japanese, Chinese…) are not ours.
    if (e.nativeEvent.isComposing) return;
    if (phone) {
      if (e.key === 'Enter' || e.key === ' ' || e.key === 'ArrowDown') {
        e.preventDefault();
        openAll();
      }
      return;
    }
    if (!open) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        openAll();
      }
      return;
    }
    if (e.key === 'Tab') close();
    // Enter belongs to the list while it is open: it never submits the form around it, and with
    // no active option it only closes the list.
    else if (e.key === 'Enter' && !choices[active]) {
      e.preventDefault();
      close();
    } else onListKey(e, choices, active, setActive, pick);
  };

  const activeId = open && !sheet && active >= 0 ? `${listId}-${active}` : undefined;

  return (
    <label className={className}>
      {label}
      <input ref={inputRef} value={value} placeholder={placeholder} autoFocus={autoFocus} autoComplete="off" spellCheck={false}
        role="combobox" aria-expanded={open} aria-controls={open ? listId : undefined} aria-autocomplete="list" aria-activedescendant={activeId}
        aria-haspopup={phone ? 'dialog' : 'listbox'} readOnly={phone}
        onChange={(e) => {
          onChange(e.target.value);
          openList(e.target.value, e.target.value.trim() ? 0 : -1);
        }}
        onClick={() => { if (!open) openAll(); }}
        onKeyDown={onKeyDown} />
      {children}
      {open && !sheet && (
        <Popover anchor={inputRef} onClose={close}>
          <ChoiceList id={listId} label={label} groups={groups} use={use} choices={choices} active={active} current={value}
            onPick={pick} onHover={setActive} />
        </Popover>
      )}
      {open && sheet && (
        <Sheet label={label} query={query} onQuery={(q) => { setQuery(q); setActive(q.trim() ? 0 : -1); }} onClose={close}
          listId={listId} active={active}
          onKey={(e) => {
            // The sheet is modal and its search field is the only stop in it: Tab stays there.
            if (e.key === 'Tab') e.preventDefault();
            else onListKey(e, choices, active, setActive, pick);
          }}>
          <ChoiceList id={listId} label={label} groups={groups} use={use} choices={choices} active={active} current={value}
            onPick={pick} onHover={setActive} />
        </Sheet>
      )}
    </label>
  );
}

/** Arrow keys (wrapping) and Enter (picks the active option) in a field that drives the list. */
function onListKey(e: KeyboardEvent<HTMLInputElement>, choices: Choice[], active: number, setActive: (n: number) => void, pick: (v: string) => void) {
  if (e.nativeEvent.isComposing) return;
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault();
    if (choices.length === 0) return;
    const step = e.key === 'ArrowDown' ? 1 : -1;
    setActive(active < 0 ? (step > 0 ? 0 : choices.length - 1) : (active + step + choices.length) % choices.length);
  } else if (e.key === 'Enter') {
    e.preventDefault();
    const choice = choices[active];
    if (choice) pick(choice.value);
  }
}

/** Whether the browser has the Popover API. Without it the overlays are not given the `popover`
 *  attribute at all, so no stylesheet rule for [popover] can hide them. */
function hasPopover(): boolean {
  return typeof HTMLElement.prototype.showPopover === 'function';
}

/** Shows a manual popover (top layer) once it is mounted. Without the Popover API (Safari < 17,
 *  Firefox < 125) the `popover` attribute means nothing and the element stays a fixed box over the
 *  page, layered by its z-index. */
function useShownPopover<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || typeof el.showPopover !== 'function' || el.matches(':popover-open')) return;
    el.showPopover();
    return () => {
      if (el.matches(':popover-open')) el.hidePopover();
    };
  }, []);
  return ref;
}

/** The desktop list: under the field (above it when there is no room), closed by a press outside. */
function Popover({ anchor, onClose, children }: { anchor: RefObject<HTMLInputElement | null>; onClose: () => void; children: ReactNode }) {
  const ref = useShownPopover<HTMLDivElement>();
  useEscape(onClose);
  const latestClose = useRef(onClose);
  useEffect(() => {
    latestClose.current = onClose;
  });

  // Follows the field when the window resizes. A scroll of anything but the list itself closes it:
  // the field may have left its scrolling dialog body, and a fixed list would float over the
  // dialog's header or footer.
  useLayoutEffect(() => {
    const place = () => {
      const el = ref.current;
      const field = anchor.current;
      if (!el || !field) return;
      const r = field.getBoundingClientRect();
      const below = window.innerHeight - r.bottom;
      const up = below < 240 && r.top > below;
      const room = Math.max(120, (up ? r.top : below) - 16);
      el.style.left = `${Math.max(8, Math.min(r.left, window.innerWidth - Math.max(r.width, 260) - 8))}px`;
      el.style.width = `${Math.max(r.width, 260)}px`;
      el.style.maxHeight = `${Math.min(room, 320)}px`;
      el.style.top = up ? '' : `${r.bottom + 6}px`;
      el.style.bottom = up ? `${window.innerHeight - r.top + 6}px` : '';
      el.style.transformOrigin = up ? 'bottom left' : 'top left';
    };
    place();
    window.addEventListener('resize', place);
    const scroll = (e: Event) => {
      const target = e.target as Node;
      if (!ref.current?.contains(target) && target !== anchor.current) latestClose.current();
    };
    window.addEventListener('scroll', scroll, true);
    return () => {
      window.removeEventListener('resize', place);
      window.removeEventListener('scroll', scroll, true);
    };
  }, [anchor, ref]);

  // A press outside the field and the list closes it. Capture phase: dialogs stop mousedown from
  // bubbling (their backdrop logic), so a document listener in the bubble phase would miss them.
  // Presses on the field's own label (its text, the help line) count as inside: they focus the field.
  useEffect(() => {
    const press = (e: PointerEvent) => {
      const target = e.target as Node;
      if (ref.current?.contains(target) || anchor.current?.closest('label')?.contains(target)) return;
      latestClose.current();
    };
    document.addEventListener('pointerdown', press, true);
    return () => document.removeEventListener('pointerdown', press, true);
  }, [anchor, ref]);

  // Presses inside the list keep the focus in the field (and the field's keyboard on screen).
  return createPortal(
    <div ref={ref} popover={hasPopover() ? 'manual' : undefined} className="system-popover" onMouseDown={(e) => e.preventDefault()}>{children}</div>,
    document.body,
  );
}

/** The phone sheet: a search field on top and the list under it; the backdrop closes it. */
function Sheet(props: {
  label: string; query: string; listId: string; active: number; children: ReactNode;
  onQuery: (q: string) => void; onClose: () => void; onKey: (e: KeyboardEvent<HTMLInputElement>) => void;
}) {
  const { t } = useTranslation();
  const ref = useShownPopover<HTMLDivElement>();
  const titleId = useId();
  useEscape(props.onClose);
  const activeId = props.active >= 0 ? `${props.listId}-${props.active}` : undefined;
  return createPortal(
    <div ref={ref} popover={hasPopover() ? 'manual' : undefined} className="system-sheet-layer" onClick={(e) => { if (e.target === e.currentTarget) props.onClose(); }}>
      <div className="system-sheet" role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <div className="system-sheet-head">
          <h2 id={titleId}>{props.label}</h2>
          <input type="search" value={props.query} autoFocus autoComplete="off" spellCheck={false} enterKeyHint="done"
            placeholder={t('systemPicker.search')} aria-label={t('systemPicker.search')}
            role="combobox" aria-expanded="true" aria-controls={props.listId} aria-autocomplete="list" aria-activedescendant={activeId}
            onChange={(e) => props.onQuery(e.target.value)} onKeyDown={props.onKey} />
        </div>
        {props.children}
      </div>
    </div>,
    document.body,
  );
}

/** The grouped options and the "Use “…”" line; ids are `${id}-${index}` in list order. */
function ChoiceList(props: {
  id: string; label: string; groups: PickerGroup[]; use: Choice | null; choices: Choice[]; active: number; current: string;
  onPick: (v: string) => void; onHover: (i: number) => void;
}) {
  const { t } = useTranslation();
  const { id, active } = props;

  // The active option stays in view as the arrows move it; it is not animated.
  useEffect(() => {
    if (active >= 0) document.getElementById(`${id}-${active}`)?.scrollIntoView({ block: 'nearest' });
  }, [id, active]);

  let index = 0;
  const option = () => {
    const i = index++;
    const c = props.choices[i]!;
    return (
      <div key={i} id={`${id}-${i}`} role="option" aria-selected={i === active}
        className={`system-option${i === active ? ' active' : ''}${c.value === props.current ? ' current' : ''}`}
        onClick={() => props.onPick(c.value)} onMouseMove={() => { if (i !== active) props.onHover(i); }}>
        {c.label}
      </div>
    );
  };

  return (
    <div id={id} role="listbox" className="system-list" aria-label={props.label}>
      {props.groups.length === 0 && <p role="presentation" className="system-none">{t('systemPicker.none')}</p>}
      {props.groups.map((g) => {
        const name = g.maker === null ? t('systemPicker.yours') : g.maker || t('systemPicker.otherMaker');
        return (
          <div key={g.maker ?? ''} role="group" aria-label={name} className="system-group">
            <div className="system-group-name" aria-hidden="true">{name}</div>
            {g.options.map(() => option())}
          </div>
        );
      })}
      {props.use && <div role="none" className="system-use">{option()}</div>}
    </div>
  );
}
