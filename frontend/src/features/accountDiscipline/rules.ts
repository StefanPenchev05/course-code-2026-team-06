import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';
import type { AuditCategory } from './types';

export const REASON_MAX_BYTES = 2000;

export const CATEGORY_LABEL: Record<AuditCategory, string> = {
  SUSPENSION: 'Suspension decisions',
  VIOLATION: 'Violations',
  FLAG: 'Flags',
};

export const VIOLATION_LABEL: Record<string, string> = {
  NO_SHOW: 'Confirmed no-show',
  LATE_CANCELLATION: 'Late cancellation (less than 24 hours before)',
};

/** The API needs a trimmed reason of 1–2000 UTF-8 bytes without NUL, so accented letters count double. */
export function reasonProblem(reason: string): string | undefined {
  const text = reason.trim();
  if (!text) return 'Give a reason; it is kept in the account’s history.';
  if (new TextEncoder().encode(text).length > REASON_MAX_BYTES) return 'This reason is too long. Shorten it to about 2000 characters.';
  if (text.includes('\u0000')) return 'The reason contains a character that is not allowed.';
  return undefined;
}

export function decisionError(error: unknown): string {
  return errorMessage(error, {
    404: 'Only the coordinator of the person’s current home faculty can suspend or reinstate them, and this account isn’t a student of your faculty now.',
    409: 'The account’s state changed meanwhile (for example it was already suspended or reinstated, or it is closed). The page now shows the latest history.',
  });
}

export const isConflict = (error: unknown) => error instanceof ApiError && error.status === 409;

/** A short reference from an ID. */
export function reference(id: string): string {
  return id.replace(/-/g, '').slice(0, 8).toUpperCase();
}

export function formatDateTime(iso: string): string {
  return new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'long', year: 'numeric', hour: '2-digit', minute: '2-digit' }).format(
    new Date(iso),
  );
}

/** Who recorded an event, without names (the API has none). */
export function actorText(kind: string | null, userId: string | null): string {
  if (kind === 'SYSTEM') return 'Recorded automatically';
  if (userId) return `Recorded by coordinator ref. ${reference(userId)}`;
  return 'Recorded by an unknown actor (older record)';
}
