import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router';
import { MAX_WATCHED_TUTORS, useAvailabilityWatch, useLiveRefresh } from '../../shared/realtime';
import { Alert, Button, Spinner } from '../../shared/ui';
import { PAGE_SIZE, tutorsApi } from './api';
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

  /**
   * Live refresh (#92): when a visible tutor's availability or rating changes, refetch the
   * same number of results in the background; the list stays on screen meanwhile.
   */
  const shown = useRef<Results | null>(null);
  useEffect(() => {
    shown.current = results;
  }, [results]);
  const refresh = useCallback(async () => {
    const before = shown.current;
    if (before?.status !== 'ok' || before.key !== key) return;
    try {
      const filtersNow = JSON.parse(key) as TutorSearchFilters;
      const items: TutorMatch[] = [];
      let hasMore = true;
      while (hasMore && items.length < Math.max(before.items.length, PAGE_SIZE)) {
        const page = await tutorsApi.search({ ...filtersNow, offset: items.length });
        items.push(...page.data);
        hasMore = page.pagination.has_more;
      }
      if (shown.current?.key === key) setResults({ key, status: 'ok', items, hasMore });
    } catch {
      // Keep what is shown; the next event or reconnection tries again.
    }
  }, [key]);
  const visibleTutors = results?.key === key && results.status === 'ok' ? results.items.slice(0, MAX_WATCHED_TUTORS).map((t) => t.id) : [];
  useAvailabilityWatch(visibleTutors, () => void refresh());
  useLiveRefresh(['availability.changed', 'activity.changed'], () => void refresh(), { entityId: visibleTutors, debounceMs: 500 });

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
