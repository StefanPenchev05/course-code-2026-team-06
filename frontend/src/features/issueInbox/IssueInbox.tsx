import { useCallback } from 'react';
import { Link } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { usePagedList } from '../../shared/hooks/usePagedList';
import { useLiveRefresh } from '../../shared/realtime';
import { Alert, Button, LiveAnnouncement, Spinner } from '../../shared/ui';
import { issuesApi } from './api';
import { IssueStatusBadge } from './IssueStatusBadge';
import { excerpt, formatDateTime, reference } from './rules';
import type { IssueStatus } from './types';

const EMPTY: Record<IssueStatus, string> = {
  OPEN: 'No open reports. New problem reports from students and tutors appear here.',
  RESOLVED: 'No resolved reports yet.',
};

/**
 * The faculty's problem reports with one status, oldest first (FR17, UC13). Render it with
 * key={status} so that changing the filter shows a loading state instead of the previous list.
 */
export function IssueInbox({ status }: { status: IssueStatus }) {
  const fetchInbox = useCallback((offset: number, signal?: AbortSignal) => issuesApi.inbox(status, offset, signal), [status]);
  const { state, reload, refresh, change, loadMore, loadingMore, moreError } = usePagedList(fetchInbox);
  // New reports and resolutions made elsewhere appear without a reload (#93).
  useLiveRefresh(['coordination.changed'], refresh);

  if (state.status === 'loading') return <Spinner label="Loading reports…" />;
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
  const announcement = <LiveAnnouncement change={change} noun={['report', 'reports']} />;
  if (state.items.length === 0) {
    return (
      <>
        {announcement}
        <p>{EMPTY[status]}</p>
      </>
    );
  }

  return (
    <>
      {announcement}
      <ul className="card-list" aria-label={status === 'OPEN' ? 'Open reports' : 'Resolved reports'}>
        {state.items.map((issue) => (
          <li key={issue.id} className="card">
            <div className="session__head">
              <h2 className="tutor-card__name">
                <Link to={`/coordinator/issues/${issue.id}`}>Report {reference(issue.id)}</Link>
              </h2>
              <IssueStatusBadge status={issue.status} />
            </div>
            <p className="card__meta">
              {issue.session_id ? 'About a session' : 'General'} · Sent {formatDateTime(issue.created_at)}
              {issue.resolved_at && ` · Resolved ${formatDateTime(issue.resolved_at)}`}
            </p>
            <p>{excerpt(issue.description)}</p>
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
