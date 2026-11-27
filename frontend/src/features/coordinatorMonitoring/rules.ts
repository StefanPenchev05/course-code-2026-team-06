import { errorMessage } from '../../shared/api/errors';
import type { EventType } from '../../shared/realtime';
import type { SessionStatus } from './types';

export const REASON_MAX_BYTES = 2000;

/**
 * Events that change the monitoring totals (#93): completions and feedback
 * (activity.changed), newly approved tutors and offerings (tutor_application.changed),
 * suspensions and other case decisions (coordination.changed).
 */
export const MONITORING_EVENTS: readonly EventType[] = ['activity.changed', 'tutor_application.changed', 'coordination.changed'];

export const STATUS_LABEL: Record<SessionStatus, string> = {
  REQUESTED: 'Requested',
  ACCEPTED: 'Confirmed',
  DECLINED: 'Declined',
  EXPIRED: 'Expired',
  CANCELLED: 'Cancelled',
  COMPLETED: 'Completed',
  NO_SHOW: 'No-show',
};

/** Only these can be cancelled; the server re-checks the status when the coordinator confirms. */
export function isCancellable(status: SessionStatus | undefined): boolean {
  return status === 'REQUESTED' || status === 'ACCEPTED';
}

/** The API limits the trimmed reason to 2000 UTF-8 bytes, so accented letters count double. */
export function reasonProblem(reason: string): string | undefined {
  const text = reason.trim();
  if (!text) return 'Give a reason; both participants can read it.';
  if (new TextEncoder().encode(text).length > REASON_MAX_BYTES) return 'This reason is too long. Shorten it to about 2000 characters.';
  if (text.includes('\u0000')) return 'The reason contains a character that is not allowed.';
  return undefined;
}

export function cancellationError(error: unknown): string {
  return errorMessage(error, {
    404: 'This session is not in your faculty or no longer exists.',
    409: 'This session can no longer be cancelled. It may have just been completed, cancelled or expired.',
  });
}

/** "4.3 / 5 (3 ratings)", or "No ratings yet". */
export function ratingText(average: number | null, count: number): string {
  if (count === 0 || average === null) return 'No ratings yet';
  return `${average.toFixed(1)} / 5 (${count} ${count === 1 ? 'rating' : 'ratings'})`;
}

export function hoursText(hours: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(hours);
}

/** Monday 00:00 of the week that contains `date`, in the browser's time zone. */
export function startOfWeek(date: Date): Date {
  const start = new Date(date);
  start.setHours(0, 0, 0, 0);
  start.setDate(start.getDate() - ((start.getDay() + 6) % 7));
  return start;
}

/** Calendar days, so a week across a daylight-saving change still starts at midnight. */
export function addDays(date: Date, days: number): Date {
  const next = new Date(date);
  next.setDate(next.getDate() + days);
  return next;
}
