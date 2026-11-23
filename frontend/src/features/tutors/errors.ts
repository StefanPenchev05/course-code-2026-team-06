import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';

/** Explains a failed tutor search; 422 "subject is not in the student's current program" gets its own sentence. */
export function searchErrorMessage(error: unknown): string {
  // Tutoring is only possible for subjects in the student's own program (AGENTS.md §6).
  if (error instanceof ApiError && error.status === 422 && /program/i.test(error.message)) {
    return "This subject isn't part of your study program, so you can't book tutoring for it. Choose a subject from your program.";
  }
  return errorMessage(error);
}
