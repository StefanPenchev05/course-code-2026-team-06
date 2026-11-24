import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';
import type { Purpose, SessionStatus } from './types';

/**
 * Booking rules mirrored from the backend (docs/scheduling.md, domain/booking.go)
 * only to offer valid choices. The server checks them again when booking.
 */

export const PURPOSES: { value: Purpose; label: string; minutes: number[] }[] = [
  { value: 'INTRODUCTION', label: 'Introduction (get to know the tutor)', minutes: [30] },
  { value: 'HOMEWORK', label: 'Homework help', minutes: [60, 90, 120] },
  { value: 'EXAM_PREPARATION', label: 'Exam preparation', minutes: [60, 90, 120] },
  { value: 'COURSE_REVIEW', label: 'Course review', minutes: [60, 90, 120] },
  { value: 'OTHER', label: 'Something else', minutes: [60, 90, 120] },
];

export const PURPOSE_LABEL: Record<Purpose, string> = Object.fromEntries(PURPOSES.map((p) => [p.value, p.label.split(' (')[0]])) as Record<
  Purpose,
  string
>;

export const MIN_NOTICE_MS = 3 * 60 * 60 * 1000;
export const LATE_CANCELLATION_MS = 24 * 60 * 60 * 1000;
const STEP_MS = 30 * 60 * 1000;
/** A little margin so a start time offered now still has 3 hours' notice when the request arrives. */
const NOTICE_MARGIN_MS = 5 * 60 * 1000;
export const DESCRIPTION_MAX_BYTES = 4000;
export const REASON_MAX_BYTES = 2000;
export const MEETING_LINK_MAX_BYTES = 2048;
export const MEETING_ROOM_MAX_BYTES = 500;

/** The meeting-link rule of domain.Meeting.Validate: http(s), a host, no spaces, no user info. */
export function validMeetingLink(value: string): boolean {
  if (!value || /\s/.test(value) || byteLength(value) > MEETING_LINK_MAX_BYTES) return false;
  try {
    const url = new URL(value);
    return (url.protocol === 'https:' || url.protocol === 'http:') && url.hostname !== '' && !url.username && !url.password;
  } catch {
    return false;
  }
}

/** Whether the tutor may mark the session completed: only once its scheduled end has passed. */
export function hasEnded(endsAt: string, now = new Date()): boolean {
  return now.getTime() >= new Date(endsAt).getTime();
}

/**
 * Start times (every 30 minutes, on the hour and half hour) inside a free
 * interval where a session of `minutes` fits and that start at least 3 hours from now.
 */
export function startOptions(window: { starts_at: string; ends_at: string }, minutes: number, now = new Date()): Date[] {
  const from = new Date(window.starts_at).getTime();
  const to = new Date(window.ends_at).getTime();
  const earliest = now.getTime() + MIN_NOTICE_MS + NOTICE_MARGIN_MS;
  const first = Math.ceil(Math.max(from, earliest) / STEP_MS) * STEP_MS;
  const options: Date[] = [];
  for (let start = first; start + minutes * 60 * 1000 <= to; start += STEP_MS) options.push(new Date(start));
  return options;
}

export function isLateCancellation(startsAt: string, now = new Date()): boolean {
  return new Date(startsAt).getTime() - now.getTime() < LATE_CANCELLATION_MS;
}

export function byteLength(value: string): number {
  return new TextEncoder().encode(value).length;
}

/** Turns a refused booking or cancellation into a sentence the student can act on. */
export function bookingError(error: unknown): string {
  if (error instanceof ApiError && error.status === 409) {
    if (/reservation/i.test(error.message)) {
      return 'You or the tutor already have a session at this time. Choose another time.';
    }
    if (/expired/i.test(error.message)) return 'This request has expired: the answer deadline has passed.';
    return 'This session was changed in the meantime (for example answered or cancelled). Reload the page to see its status.';
  }
  if (error instanceof ApiError && error.status === 422) {
    const message = error.message;
    if (/three hours/i.test(message)) return 'Sessions must be booked at least 3 hours in advance. Choose a later time.';
    if (/fit the window/i.test(message)) return 'That time is no longer free or doesn’t fit the tutor’s free time. Choose another time.';
    if (/both programs/i.test(message)) return 'This subject isn’t part of both your program and the tutor’s, so it can’t be booked.';
    if (/no longer approved/i.test(message)) return 'The tutor no longer teaches this subject.';
    if (/yourself/i.test(message)) return 'You can’t book a session with yourself.';
    if (/meeting url/i.test(message)) return 'Enter a meeting link that starts with https:// (for example a Teams or Zoom link).';
    if (/room or location/i.test(message)) return 'Enter the room or place where you will meet.';
    const text = message.replace(/^invalid booking:\s*/i, '');
    if (text) return text.charAt(0).toUpperCase() + text.slice(1);
  }
  if (error instanceof ApiError && error.status === 404) return 'This tutor or session isn’t available any more.';
  return errorMessage(error);
}

/** Status in words for the person viewing the session. */
export const STATUS_LABEL: Record<SessionStatus, string> = {
  REQUESTED: 'Waiting for the tutor',
  ACCEPTED: 'Confirmed',
  DECLINED: 'Declined',
  EXPIRED: 'Expired (no answer)',
  CANCELLED: 'Cancelled',
  COMPLETED: 'Completed',
  NO_SHOW: 'No-show',
};


/** The status in words for whoever is looking: a tutor is waited on, a student waits. */
export function statusLabel(session: { status: SessionStatus; viewer_role?: 'STUDENT' | 'TUTOR' }): string {
  if (session.status === 'REQUESTED' && session.viewer_role === 'TUTOR') return 'Waiting for your answer';
  return STATUS_LABEL[session.status] ?? session.status;
}

/** Only http(s) links become clickable; anything else (e.g. "javascript:") is shown as text. */
export function isWebLink(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol === 'https:' || url.protocol === 'http:';
  } catch {
    return false;
  }
}
