import { useCallback } from 'react';
import { Link } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { useLiveRefresh } from '../../shared/realtime';
import { Alert, Button, LiveAnnouncement, Spinner } from '../../shared/ui';
import { applicationsApi } from './api';
import { ApplicationStatusBadge } from './ApplicationStatusBadge';
import { applicantReference, formatDateTime } from './format';
import type { ApplicationStatus } from './types';
import { usePagedList } from './usePagedList';

const EMPTY_MESSAGES: Record<ApplicationStatus, string> = {
  PENDING: 'No applications are waiting for review.',
  APPROVED: 'No approved applications yet.',
  REJECTED: 'No rejected applications.',
};

/**
 * Coordinator queue for one status. The API returns only applications from the
 * coordinator's own faculty. Render it with key={status} so that changing the
 * filter shows a loading state instead of the previous list.
 */
export function ApplicationQueue({ status }: { status: ApplicationStatus }) {
  const fetchQueue = useCallback(
    (offset: number, signal?: AbortSignal) => applicationsApi.queue(status, offset, signal),
    [status],
  );
  const { state, reload, refresh, change, loadMore, loadingMore, moreError } = usePagedList(fetchQueue);
  // New applications and decisions by other coordinators appear without a reload (#93).
  useLiveRefresh(['tutor_application.changed'], refresh);

  if (state.status === 'loading') return <Spinner label="Loading applications…" />;
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
  const announcement = <LiveAnnouncement change={change} noun={['application', 'applications']} />;
  if (state.items.length === 0) {
    return (
      <>
        {announcement}
        <p>{EMPTY_MESSAGES[status]}</p>
      </>
    );
  }

  return (
    <>
      {announcement}
      <ul className="card-list">
        {state.items.map((application) => (
          <li key={application.id} className="card">
            <div className="application__head">
              <h2>
                <Link to={`/coordinator/tutor-applications/${application.id}`}>{application.subject_name}</Link>
              </h2>
              <ApplicationStatusBadge status={application.status} />
            </div>
            <dl className="details">
              <dt>Applicant ref.</dt>
              <dd>{applicantReference(application.student_id)}</dd>
              <dt>Submitted</dt>
              <dd>{formatDateTime(application.submitted_at)}</dd>
              {application.reviewed_at && (
                <>
                  <dt>Decided</dt>
                  <dd>{formatDateTime(application.reviewed_at)}</dd>
                </>
              )}
            </dl>
          </li>
        ))}
      </ul>
      {moreError !== null && <Alert>{errorMessage(moreError)}</Alert>}
      {state.hasMore && (
        <Button variant="secondary" loading={loadingMore} onClick={loadMore}>
          Show more applications
        </Button>
      )}
    </>
  );
}
