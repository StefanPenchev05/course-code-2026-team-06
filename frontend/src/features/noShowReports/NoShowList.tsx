import { useCallback } from 'react';
import { Link } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { usePagedList } from '../../shared/hooks/usePagedList';
import { Alert, Button, Spinner } from '../../shared/ui';
import { noShowApi } from './api';
import { NoShowStatusBadge } from './NoShowStatusBadge';
import { excerpt, formatDateTime, reference } from './rules';
import type { NoShowStatus } from './types';

const EMPTY: Record<NoShowStatus, string> = {
  PENDING: 'No no-show reports are waiting for a decision.',
  CONFIRMED: 'No confirmed no-shows.',
  REJECTED: 'No rejected no-show reports.',
};

const LIST_NAME: Record<NoShowStatus, string> = {
  PENDING: 'No-show reports waiting for a decision',
  CONFIRMED: 'Confirmed no-show reports',
  REJECTED: 'Rejected no-show reports',
};

/**
 * No-show reports with one status for sessions taught in the coordinator's faculty, oldest first
 * (FR17, UC13). Render it with key={status} so that changing the filter shows a loading state.
 */
export function NoShowList({ status }: { status: NoShowStatus }) {
  const fetchReports = useCallback((offset: number, signal?: AbortSignal) => noShowApi.list(status, offset, signal), [status]);
  const { state, reload, loadMore, loadingMore, moreError } = usePagedList(fetchReports);

  if (state.status === 'loading') return <Spinner label="Loading no-show reports…" />;
  if (state.status === 'error') {
    return (
      <Alert
        action={
          <Button variant="secondary" onClick={reload}>
            Try again
          </Button>
        }
      >
        {errorMessage(state.error)}
      </Alert>
    );
  }
  if (state.items.length === 0) return <p>{EMPTY[status]}</p>;

  return (
    <>
      <ul className="card-list" aria-label={LIST_NAME[status]}>
        {state.items.map((report) => (
          <li key={report.id} className="card">
            <div className="session__head">
              <h2 className="tutor-card__name">
                <Link to={`/coordinator/no-show-reports/${report.id}`}>No-show report {reference(report.id)}</Link>
              </h2>
              <NoShowStatusBadge status={report.status} />
            </div>
            <p className="card__meta">
              Session {reference(report.session_id)} · Reported {formatDateTime(report.reported_at)}
              {report.reviewed_at && ` · Decided ${formatDateTime(report.reviewed_at)}`}
            </p>
            <p>{excerpt(report.description)}</p>
          </li>
        ))}
      </ul>
      {moreError !== null && <Alert>{errorMessage(moreError)}</Alert>}
      {state.hasMore && (
        <Button variant="secondary" loading={loadingMore} onClick={loadMore}>
          Show more reports
        </Button>
      )}
    </>
  );
}
