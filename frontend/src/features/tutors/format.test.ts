import { describe, expect, it } from 'vitest';
import { groupByDay, ratingText, timeRange } from './format';
import type { FreeSlot } from './types';

function slot(starts_at: string, ends_at: string): FreeSlot {
  return { id: 's', tutor_id: 't', starts_at, ends_at, allowed_mode: 'ONLINE', is_recurring: false };
}

describe('ratingText', () => {
  it('shows the average with the number of ratings', () => {
    expect(ratingText({ average_rating: 4.25, rating_count: 12 })).toBe('4.3 ★ (12 ratings)');
    expect(ratingText({ average_rating: 5, rating_count: 1 })).toBe('5.0 ★ (1 rating)');
  });

  it('says when there are no ratings yet', () => {
    expect(ratingText({ average_rating: null, rating_count: 0 })).toBe('No ratings yet');
  });
});

describe('free time display', () => {
  // 23:30 UTC on 1 Dec is already 00:30 on 2 Dec in Luxembourg (UTC+1 in winter).
  const lateEvening = slot('2026-12-01T23:30:00Z', '2026-12-02T00:15:00Z');
  const nextMorning = slot('2026-12-02T08:00:00Z', '2026-12-02T09:00:00Z');

  it('groups intervals by the local calendar day', () => {
    const groups = groupByDay([lateEvening, nextMorning], 'Europe/Luxembourg', 'en-GB');
    expect(groups).toHaveLength(1);
    expect(groups[0].label).toBe('Wednesday 2 December');
    expect(groups[0].slots).toHaveLength(2);

    expect(groupByDay([lateEvening, nextMorning], 'UTC', 'en-GB').map((g) => g.label)).toEqual([
      'Tuesday 1 December',
      'Wednesday 2 December',
    ]);
  });

  it('shows times in the given time zone', () => {
    expect(timeRange(nextMorning, 'Europe/Luxembourg', 'en-GB')).toBe('09:00–10:00');
    expect(timeRange(nextMorning, 'UTC', 'en-GB')).toBe('08:00–09:00');
  });
});
