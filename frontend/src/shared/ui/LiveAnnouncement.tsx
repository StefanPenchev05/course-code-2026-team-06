import type { ListChange } from '../hooks/usePagedList';

interface LiveAnnouncementProps {
  /** From usePagedList: what the last live refresh changed, or null. */
  change: ListChange | null;
  /** The listed thing, singular and plural, e.g. ['report', 'reports']. */
  noun: readonly [string, string];
}

/**
 * Says politely what a live update changed in a list (#93), so screen-reader users
 * learn about rows that appeared or left without anyone reloading. Visually quiet;
 * empty until something changes.
 */
export function LiveAnnouncement({ change, noun }: LiveAnnouncementProps) {
  const parts: string[] = [];
  if (change?.added) parts.push(`${change.added} new ${change.added === 1 ? noun[0] : noun[1]}.`);
  if (change?.removed) parts.push(`${change.removed} ${change.removed === 1 ? `${noun[0]} is` : `${noun[1]} are`} no longer in this list.`);
  return (
    <p className="live-announcement" role="status" aria-live="polite">
      {parts.length ? `Updated: ${parts.join(' ')}` : ''}
    </p>
  );
}
