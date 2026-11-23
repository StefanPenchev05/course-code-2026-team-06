/**
 * Display helpers for tutor applications.
 *
 * Times are shown in Luxembourg time for everyone (Tutti is used at the
 * University of Luxembourg), so the output is the same in every browser,
 * in tests and in CI.
 */
const dateTimeFormat = new Intl.DateTimeFormat('en-GB', {
  dateStyle: 'medium',
  timeStyle: 'short',
  timeZone: 'Europe/Luxembourg',
});

/** "2026-11-20T10:00:00Z" → "20 Nov 2026, 11:00". Returns the input unchanged if it isn't a valid timestamp. */
export function formatDateTime(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? iso : dateTimeFormat.format(date);
}

/**
 * The application API identifies the applicant only by user ID (no name).
 * Show a short, stable reference so a coordinator can tell applications apart.
 * Replace this with the name once the API returns it.
 */
export function applicantReference(studentId: string): string {
  return studentId.slice(0, 8).toUpperCase();
}
