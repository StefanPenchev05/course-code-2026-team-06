import { errorMessage } from '../../shared/api/errors';

export const DESCRIPTION_MAX_BYTES = 2000;

/** The API needs 1–2000 UTF-8 bytes after trimming and no NUL, so accented letters count double. */
export function descriptionProblem(description: string): string | undefined {
  const text = description.trim();
  if (!text) return 'Describe the problem.';
  if (new TextEncoder().encode(text).length > DESCRIPTION_MAX_BYTES) return 'This description is too long. Shorten it to about 2000 characters.';
  if (text.includes('\u0000')) return 'The description contains a character that is not allowed.';
  return undefined;
}

export function reportError(error: unknown): string {
  return errorMessage(error, {
    403: 'Your account can’t send reports right now, for example while your registration is waiting for approval or your account is suspended.',
    404: 'This session isn’t one of yours, or it doesn’t exist. Send the report without linking it to a session.',
    409: 'The report could not be saved. Please try again.',
  });
}

/** A short reference the student can quote, from the report's ID. */
export function reportReference(id: string): string {
  return id.replace(/-/g, '').slice(0, 8).toUpperCase();
}
