import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';

/**
 * Explains a refused availability change. The API answers 409 for three
 * different reasons (backend/internal/scheduling/domain/availability.go) and
 * 422 with the rule that failed (for example a window outside the semester).
 */
export function availabilityError(error: unknown): string {
  if (error instanceof ApiError && error.status === 409) {
    if (/active session/i.test(error.message)) {
      return 'A student has a booking that depends on this time, so it can’t be changed or removed. Deal with that session first (cancel it, or wait until it is over).';
    }
    if (/overlap/i.test(error.message)) return 'This overlaps another free time of yours. Choose a time that doesn’t overlap.';
    return 'Your availability changed at the same time somewhere else. Reload the page and try again.';
  }
  if (error instanceof ApiError && error.status === 422 && error.message) {
    const text = error.message.replace(/^invalid availability:\s*/i, '');
    return text.charAt(0).toUpperCase() + text.slice(1);
  }
  if (error instanceof ApiError && error.status === 404) {
    return 'This time no longer exists. It may have been removed already. Reload the page.';
  }
  return errorMessage(error);
}
