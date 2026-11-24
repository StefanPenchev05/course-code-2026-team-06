import type { Tutor } from './types';

/** "4.5 ★ (12 ratings)", or "No ratings yet". Ratings are aggregates; individual comments stay private. */
export function ratingText(tutor: Pick<Tutor, 'average_rating' | 'rating_count'>): string {
  if (tutor.rating_count === 0 || tutor.average_rating === null) return 'No ratings yet';
  const count = `${tutor.rating_count} ${tutor.rating_count === 1 ? 'rating' : 'ratings'}`;
  return `${tutor.average_rating.toFixed(1)} ★ (${count})`;
}
