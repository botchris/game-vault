import type { ReactNode } from 'react';

/** The header every page starts with: its title (the page's one h1), an optional count beside it,
 *  and the page's actions at the end of the row. */
export function PageHeader({ title, count, actions }: { title: ReactNode; count?: ReactNode; actions?: ReactNode }) {
  return (
    <header className="page-head">
      <h1 className="page-title">{title}</h1>
      {count !== undefined && <span className="page-count">{count}</span>}
      {actions && (
        <>
          <span className="spacer" />
          {actions}
        </>
      )}
    </header>
  );
}
