import { renewsIn } from './providerStatus.ts';

type Translate = (key: string, options?: Record<string, string>) => string;

/** A health issue as the pure helpers need it (see HealthIssue in the API). */
export interface IssueLike {
  level: string;
  kind: string;
  source: string;
  reason: string;
  params: Record<string, string>;
  message: string;
  target: string;
  targetId: string;
}

/** The worst level among the issues, for the sidebar's dot; null when there are none. */
export function worstLevel(issues: { level: string }[]): 'error' | 'warning' | null {
  if (issues.some((i) => i.level === 'error')) return 'error';
  return issues.length > 0 ? 'warning' : null;
}

/** Where in the app an issue can be fixed, or null when nowhere this version knows. A source's
 *  issue opens that source; the backups' scroll to the Backups card. */
export function issueHref(issue: { target: string; targetId: string }): string | null {
  switch (issue.target) {
    case 'providers': return '#/providers';
    case 'sources': return issue.targetId ? `#/sources/${encodeURIComponent(issue.targetId)}` : '#/sources';
    case 'system/backups': return '#/system/backups';
    default: return null;
  }
}

/** What has the problem: the provider's or source's name, or the part of the app in the UI's
 *  language (the backups, the disk). */
export function issueTitle(issue: Pick<IssueLike, 'kind' | 'source'>, t: Translate): string {
  if (issue.kind === 'backups' || issue.kind === 'disk') return t(`health.kinds.${issue.kind}`);
  return issue.source;
}

/** The reason codes the server sends (health.Reason, internal/application/health/health.go), each
 *  translated under health.reasons. TestHealthReasons_webAgrees (internal/application/health)
 *  keeps the server's list, this one and the translations equal. */
export const Reason = {
  ProviderQuota: 'provider_quota',
  ProviderRateLimited: 'provider_rate_limited',
  ProviderUnavailable: 'provider_unavailable',
  ProviderSettings: 'provider_settings',
  SourceSignedOut: 'source_signed_out',
  SourceSyncFailed: 'source_sync_failed',
  SourceKeepAliveFailed: 'source_keepalive_failed',
  SourceListFailed: 'source_list_failed',
  BackupFailed: 'backup_failed',
  BackupStale: 'backup_stale',
  DiskLow: 'disk_low',
  DiskCritical: 'disk_critical',
} as const;

const reasons = new Set<string>(Object.values(Reason));

/** The issue's reason in the UI's language ("Monthly allowance used up; renews in 27 days"), or the
 *  server's English message for a reason this version does not know. */
export function reasonText(issue: Pick<IssueLike, 'reason' | 'params' | 'message'>, t: Translate, now: Date, locale: string): string {
  if (!reasons.has(issue.reason)) return issue.message;
  const params: Record<string, string> = { ...issue.params };
  if (params.until) params.until = renewsIn(new Date(params.until), now, locale);
  return t(`health.reasons.${issue.reason}`, params);
}

/** The accessible label of a page's health dot: "System: error". */
export function dotLabel(level: 'error' | 'warning', page: string, t: Translate): string {
  return t('health.dot', { page, level: t(`health.${level}`) });
}

/** How long ago a problem began, in the largest unit that fits ("3 days ago"). */
export function sinceText(since: Date, now: Date, locale: string): string {
  const ms = Math.max(0, now.getTime() - since.getTime());
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
  const hour = 3600 * 1000;
  if (ms >= 36 * hour) return rtf.format(-Math.round(ms / (24 * hour)), 'day');
  if (ms >= 90 * 60 * 1000) return rtf.format(-Math.round(ms / hour), 'hour');
  return rtf.format(-Math.max(1, Math.round(ms / 60000)), 'minute');
}
