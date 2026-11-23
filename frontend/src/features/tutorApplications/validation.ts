/**
 * Form checks for a quick, friendly answer. They mirror the backend rules
 * (backend/internal/tutoring), but the backend still makes the final decision.
 */

export const DESCRIPTION_MAX_BYTES = 4000;
export const REJECTION_REASON_MAX_BYTES = 2000;

/** Size in UTF-8 bytes. The backend limits bytes, not characters (é counts as 2). */
export function byteLength(text: string): number {
  let bytes = 0;
  for (const char of text) {
    const code = char.codePointAt(0) ?? 0;
    bytes += code < 0x80 ? 1 : code < 0x800 ? 2 : code < 0x10000 ? 3 : 4;
  }
  return bytes;
}

// Same format as domain.ParseGrade: two digits, optionally up to two decimals.
const GRADE_PATTERN = /^\d{2}(\.\d{1,2})?$/;

/**
 * Turns what the student typed into the number the API expects, or null if
 * it isn't a valid grade. Accepts a decimal comma ("17,5"). Valid: 15.00–20.00.
 */
export function parseGrade(input: string): number | null {
  const normalized = input.trim().replace(',', '.');
  if (!GRADE_PATTERN.test(normalized)) return null;
  const grade = Number(normalized);
  return grade >= 15 && grade <= 20 ? grade : null;
}

export interface ApplicationFieldErrors {
  grade?: string;
  description?: string;
}

export function validateApplication(grade: string, description: string): ApplicationFieldErrors {
  const errors: ApplicationFieldErrors = {};
  if (!grade.trim()) {
    errors.grade = 'Enter your grade in this subject.';
  } else if (parseGrade(grade) === null) {
    errors.grade = 'Enter a grade from 15 to 20 with at most two decimals, for example 17.5.';
  }

  const text = description.trim();
  if (!text) {
    errors.description = 'Tell the coordinator why you want to tutor this subject.';
  } else if (byteLength(text) > DESCRIPTION_MAX_BYTES) {
    errors.description = 'This text is too long. Shorten it to about 4,000 characters.';
  }
  return errors;
}

/** Returns an error message, or undefined if the reason can be sent. */
export function validateRejectionReason(reason: string): string | undefined {
  const text = reason.trim();
  if (!text) return 'Give a reason. The student will see it.';
  if (byteLength(text) > REJECTION_REASON_MAX_BYTES) return 'The reason is too long. Shorten it to about 2,000 characters.';
  return undefined;
}
