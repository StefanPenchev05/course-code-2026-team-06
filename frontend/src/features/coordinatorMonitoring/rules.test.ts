import { describe, expect, it } from 'vitest';
import { ApiError } from '../../shared/api/client';
import { addDays, cancellationError, isCancellable, ratingText, reasonProblem, startOfWeek } from './rules';

describe('reasonProblem', () => {
  it('needs a reason of 1–2000 UTF-8 bytes after trimming, like the API', () => {
    expect(reasonProblem('   ')).toBe('Give a reason; both participants can read it.');
    expect(reasonProblem('  Outside faculty policy.  ')).toBeUndefined();
    expect(reasonProblem('a'.repeat(2000))).toBeUndefined();
    expect(reasonProblem('a'.repeat(2001))).toBe('This reason is too long. Shorten it to about 2000 characters.');
    // "é" is two bytes in UTF-8, so 1001 of them are over the limit.
    expect(reasonProblem('é'.repeat(1000))).toBeUndefined();
    expect(reasonProblem('é'.repeat(1001))).toBeDefined();
    expect(reasonProblem('ok\u0000')).toBe('The reason contains a character that is not allowed.');
  });
});

describe('isCancellable', () => {
  it('allows only requested and confirmed sessions', () => {
    expect(isCancellable('REQUESTED')).toBe(true);
    expect(isCancellable('ACCEPTED')).toBe(true);
    for (const status of ['DECLINED', 'EXPIRED', 'CANCELLED', 'COMPLETED', 'NO_SHOW'] as const) expect(isCancellable(status)).toBe(false);
    expect(isCancellable(undefined)).toBe(false);
  });
});

describe('cancellationError', () => {
  it('explains a lifecycle conflict and a session outside the faculty', () => {
    expect(cancellationError(new ApiError(409, 'session cannot be cancelled in its current state'))).toBe(
      'This session can no longer be cancelled. It may have just been completed, cancelled or expired.',
    );
    expect(cancellationError(new ApiError(404, 'session not found'))).toBe('This session is not in your faculty or no longer exists.');
  });
});

describe('ratingText', () => {
  it('shows one decimal and the number of ratings', () => {
    expect(ratingText(4.67, 3)).toBe('4.7 / 5 (3 ratings)');
    expect(ratingText(5, 1)).toBe('5.0 / 5 (1 rating)');
    expect(ratingText(null, 0)).toBe('No ratings yet');
  });
});

describe('startOfWeek and addDays', () => {
  it('starts weeks on Monday at midnight, local time', () => {
    const wednesday = new Date(2026, 10, 25, 17, 30); // Wed 25 Nov 2026
    const monday = startOfWeek(wednesday);
    expect([monday.getFullYear(), monday.getMonth(), monday.getDate(), monday.getHours(), monday.getMinutes()]).toEqual([2026, 10, 23, 0, 0]);
    const sunday = new Date(2026, 10, 29, 23, 59);
    expect(startOfWeek(sunday).getDate()).toBe(23);
    expect(startOfWeek(monday).getTime()).toBe(monday.getTime());
  });

  it('adds calendar days, so midnight stays midnight across a clock change', () => {
    const beforeChange = new Date(2026, 9, 19); // Mon 19 Oct 2026; clocks change on 25 Oct in Europe
    const next = addDays(beforeChange, 7);
    expect([next.getDate(), next.getHours()]).toEqual([26, 0]);
    expect(addDays(next, -7).getTime()).toBe(beforeChange.getTime());
  });
});
