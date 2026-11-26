import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';
import type { IssueStatus } from './types';

export const NOTE_MAX_BYTES = 2000;

export const STATUS_LABEL: Record<IssueStatus, string> = { OPEN: 'Open', RESOLVED: 'Resolved' };

export function isIssueStatus(value: string): value is IssueStatus {
  return value === 'OPEN' || value === 'RESOLVED';
}

/** The API needs a trimmed note of 1–2000 UTF-8 bytes without NUL, so accented letters count double. */
export function noteProblem(note: string): string | undefined {
  const text = note.trim();
  if (!text) return 'Write what you did or decided; it is stored with the report.';
  if (new TextEncoder().encode(text).length > NOTE_MAX_BYTES) return 'This note is too long. Shorten it to about 2000 characters.';
  if (text.includes('\u0000')) return 'The note contains a character that is not allowed.';
  return undefined;
}

/** 404 = another faculty or no such report; 422 = malformed ID in the URL. */
export function loadError(error: unknown): string {
  return errorMessage(error, {
    404: 'This report doesn’t exist or belongs to another faculty.',
    422: 'This report doesn’t exist or belongs to another faculty.',
  });
}

export function resolveError(error: unknown): string {
  return errorMessage(error, {
    404: 'This report is no longer available to you.',
    409: 'This report was already resolved, for example by another coordinator. The page now shows its current state.',
  });
}

export const isConflict = (error: unknown) => error instanceof ApiError && error.status === 409;

/** A short reference from an ID, the same one the student saw after sending the report. */
export function reference(id: string): string {
  return id.replace(/-/g, '').slice(0, 8).toUpperCase();
}

/** The first line of a description, cut at about 140 characters for the inbox; "…" when there is more. */
export function excerpt(description: string, max = 140): string {
  const text = description.trim();
  const line = text.split('\n')[0].trimEnd();
  const chars = [...line];
  const shown = chars.length > max ? chars.slice(0, max).join('').trimEnd() : line;
  return shown.length < text.length ? `${shown}…` : shown;
}

export function formatDateTime(iso: string): string {
  return new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'long', year: 'numeric', hour: '2-digit', minute: '2-digit' }).format(
    new Date(iso),
  );
}
