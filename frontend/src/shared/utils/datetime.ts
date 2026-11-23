/** Date and time display helpers shared by features. Absolute instants are shown in a given IANA zone. */

export type SessionMode = 'ONLINE' | 'ONSITE' | 'BOTH';

export const MODE_LABEL: Record<SessionMode, string> = {
  ONLINE: 'Online',
  ONSITE: 'On site',
  BOTH: 'Online or on site',
};

/** Anything with an absolute start and end, like a free interval or a published window. */
export interface TimeWindow {
  starts_at: string;
  ends_at: string;
}

export interface DayGroup<T> {
  key: string;
  label: string;
  items: T[];
}

/** Groups windows by calendar day in the given time zone (the browser's by default). */
export function groupByDay<T extends TimeWindow>(windows: T[], timeZone?: string, locale?: string): DayGroup<T>[] {
  const dayKey = new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' });
  const dayLabel = new Intl.DateTimeFormat(locale, { timeZone, weekday: 'long', day: 'numeric', month: 'long' });
  const groups = new Map<string, DayGroup<T>>();
  for (const window of windows) {
    const start = new Date(window.starts_at);
    const key = dayKey.format(start);
    let group = groups.get(key);
    if (!group) {
      group = { key, label: dayLabel.format(start), items: [] };
      groups.set(key, group);
    }
    group.items.push(window);
  }
  return [...groups.values()];
}

/** "14:00–15:30" in the given time zone. */
export function timeRange(window: TimeWindow, timeZone?: string, locale?: string): string {
  const time = new Intl.DateTimeFormat(locale, { timeZone, hour: '2-digit', minute: '2-digit' });
  return `${time.format(new Date(window.starts_at))}–${time.format(new Date(window.ends_at))}`;
}

export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}
