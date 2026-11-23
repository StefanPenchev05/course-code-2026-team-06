import type { FreeSlot, SessionMode, Tutor } from './types';

export const MODE_LABEL: Record<SessionMode, string> = {
  ONLINE: 'Online',
  ONSITE: 'On site',
  BOTH: 'Online or on site',
};

/** "4.5 ★ (12 ratings)", or "No ratings yet". Ratings are aggregates; individual comments stay private. */
export function ratingText(tutor: Pick<Tutor, 'average_rating' | 'rating_count'>): string {
  if (tutor.rating_count === 0 || tutor.average_rating === null) return 'No ratings yet';
  const count = `${tutor.rating_count} ${tutor.rating_count === 1 ? 'rating' : 'ratings'}`;
  return `${tutor.average_rating.toFixed(1)} ★ (${count})`;
}

export interface DayGroup {
  key: string;
  label: string;
  slots: FreeSlot[];
}

/** Groups free intervals by calendar day in the given time zone (the browser's by default). */
export function groupByDay(slots: FreeSlot[], timeZone?: string, locale?: string): DayGroup[] {
  const dayKey = new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' });
  const dayLabel = new Intl.DateTimeFormat(locale, { timeZone, weekday: 'long', day: 'numeric', month: 'long' });
  const groups = new Map<string, DayGroup>();
  for (const slot of slots) {
    const start = new Date(slot.starts_at);
    const key = dayKey.format(start);
    let group = groups.get(key);
    if (!group) {
      group = { key, label: dayLabel.format(start), slots: [] };
      groups.set(key, group);
    }
    group.slots.push(slot);
  }
  return [...groups.values()];
}

/** "14:00–15:30" in the given time zone. */
export function timeRange(slot: FreeSlot, timeZone?: string, locale?: string): string {
  const time = new Intl.DateTimeFormat(locale, { timeZone, hour: '2-digit', minute: '2-digit' });
  return `${time.format(new Date(slot.starts_at))}–${time.format(new Date(slot.ends_at))}`;
}

export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}
