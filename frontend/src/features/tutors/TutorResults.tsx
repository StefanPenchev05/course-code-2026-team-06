import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { Alert, Button, Spinner } from '../../shared/ui';
import { tutorsApi } from './api';
import { searchErrorMessage } from './errors';
import { ratingText } from './format';
import type { TutorMatch, TutorSearchFilters } from './types';

type Results =
  | { key: string; status: 'ok'; items: TutorMatch[]; hasMore: boolean }
  | { key: string; status: 'error'; message: string };

/** Lists approved, active tutors for the filters; "Show more" loads the next page. */
export function TutorResults({ filters }: { filters: TutorSearchFilters }) {
  const key = JSON.stringify(filters);
  const [results, setResults] = useState<Results | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreError, setMoreError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    // Re-run only when a filter value changes, not when the parent passes a new but equal object.
    const current = JSON.parse(key) as TutorSearchFilters;
    tutorsApi.search(current, controller.signal).then(
      (page) => setResults({ key, status: 'ok', items: page.data, hasMore: page.pagination.has_more }),
      (error: unknown) => {
        if (!controller.signal.aborted) setResults({ key, status: 'error', message: searchErrorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [key]);

  const current = results?.key === key ? results : null;
  if (!current) return <Spinner label="Finding tutors…" />;
  if (current.status === 'error') return <Alert>{current.message}</Alert>;
  if (current.items.length === 0) {
    return (
      <Alert kind="info" title="No tutors found">
        Nobody teaches this subject with these filters yet. Try removing a filter, or check again later.
      </Alert>
    );
  }

  async function showMore() {
    if (current?.status !== 'ok') return;
    setLoadingMore(true);
    setMoreError(null);
    try {
      const page = await tutorsApi.search({ ...filters, offset: current.items.length });
      setResults({ key, status: 'ok', items: [...current.items, ...page.data], hasMore: page.pagination.has_more });
    } catch (error) {
      setMoreError(searchErrorMessage(error));
    } finally {
      setLoadingMore(false);
    }
  }

  return (
    <section aria-labelledby="results-heading">
      <h2 id="results-heading">
        {current.items.length} {current.items.length === 1 ? 'tutor' : 'tutors'}
        {current.hasMore ? ' so far' : ''}
      </h2>
      <ul className="card-list">
        {current.items.map((tutor) => (
          <li key={tutor.id} className="card">
            <h3 className="card__title">
              <Link to={`/tutors/${tutor.id}?subject_id=${encodeURIComponent(filters.subject_id)}`}>{tutor.name}</Link>
            </h3>
            <p className="card__meta">
              {tutor.offering.subject_name} · {ratingText(tutor)}
            </p>
            {tutor.offering.description && <p>{tutor.offering.description}</p>}
          </li>
        ))}
      </ul>
      {moreError && <Alert>{moreError}</Alert>}
      {current.hasMore && (
        <Button variant="secondary" onClick={() => void showMore()} loading={loadingMore}>
          Show more tutors
        </Button>
      )}
    </section>
  );
}
