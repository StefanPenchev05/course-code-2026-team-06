import type { ActivityTotals } from './types';
import { hoursText } from './rules';

/** Completed sessions, hours, average rating and number of ratings, plus any counts placed before them. */
export function Totals({ totals, before = [] }: { totals: ActivityTotals; before?: [string, number][] }) {
  return (
    <dl className="stats">
      {before.map(([label, value]) => (
        <div key={label} className="stats__item">
          <dt>{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
      <div className="stats__item">
        <dt>Completed sessions</dt>
        <dd>{totals.completed_sessions}</dd>
      </div>
      <div className="stats__item">
        <dt>Hours taught</dt>
        <dd>{hoursText(totals.hours_taught)}</dd>
      </div>
      <div className="stats__item">
        <dt>Average rating</dt>
        {totals.average_rating === null ? (
          <dd className="stats__empty">No ratings yet</dd>
        ) : (
          <dd>{totals.average_rating.toFixed(1)} / 5</dd>
        )}
      </div>
      <div className="stats__item">
        <dt>Ratings</dt>
        <dd>{totals.rating_count}</dd>
      </div>
    </dl>
  );
}
