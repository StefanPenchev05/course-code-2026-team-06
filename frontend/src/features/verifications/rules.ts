import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';
import type { VerificationStatus } from './types';

export const REASON_MAX_BYTES = 2000;

export const STATUS_LABEL: Record<VerificationStatus, string> = {
  PENDING: 'Waiting for review',
  APPROVED: 'Approved',
  REJECTED: 'Rejected',
};

/** The API needs a trimmed reason of 1–2000 UTF-8 bytes without NUL, so accented letters count double. */
export function reasonProblem(reason: string): string | undefined {
  const text = reason.trim();
  if (!text) return 'Give a reason; the student sees it and can correct their details.';
  if (new TextEncoder().encode(text).length > REASON_MAX_BYTES) return 'This reason is too long. Shorten it to about 2000 characters.';
  if (text.includes('\u0000')) return 'The reason contains a character that is not allowed.';
  return undefined;
}

/** 404 = not in this faculty (or no such verification); 422 = malformed ID in the URL. */
export function loadError(error: unknown): string {
  return errorMessage(error, {
    404: 'This verification doesn’t exist or belongs to another faculty.',
    422: 'This verification doesn’t exist or belongs to another faculty.',
  });
}

export function decisionError(error: unknown): string {
  return errorMessage(error, {
    404: 'This verification is no longer available to you. The student may have moved to another faculty.',
    409: 'This verification was already decided, for example by another coordinator. The page now shows its current state.',
  });
}

export const isConflict = (error: unknown) => error instanceof ApiError && error.status === 409;

export function formatDateTime(iso: string): string {
  return new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'long', year: 'numeric', hour: '2-digit', minute: '2-digit' }).format(
    new Date(iso),
  );
}
