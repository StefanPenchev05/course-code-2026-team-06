import { Link } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { usePagedList } from '../../shared/hooks/usePagedList';
import { useLiveRefresh } from '../../shared/realtime';
import { Alert, Button, LiveAnnouncement, Spinner } from '../../shared/ui';
import { verificationsApi } from './api';
import { formatDateTime } from './rules';
import { useProgramNames } from './useProgramNames';

/** Pending student registrations of the coordinator's faculty, oldest first (FR2, UC01). */
export function VerificationQueue() {
  const { state, reload, refresh, change, loadMore, loadingMore, moreError } = usePagedList(verificationsApi.pending);
  // New registrations and decisions made elsewhere appear without a reload (#93).
  useLiveRefresh(['coordination.changed'], refresh);
  const programs = useProgramNames();

  if (state.status === 'loading') return <Spinner label="Loading registrations…" />;
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
  const announcement = <LiveAnnouncement change={change} noun={['registration', 'registrations']} />;
  if (state.items.length === 0) {
    return (
      <>
        {announcement}
        <p>No registrations are waiting for review.</p>
      </>
    );
  }

  return (
    <>
      {announcement}
      <ul className="card-list" aria-label="Registrations waiting for review">
        {state.items.map((verification) => (
          <li key={verification.id} className="card">
            <h2 className="tutor-card__name">
              <Link to={`/coordinator/verifications/${verification.id}`}>{verification.name || 'Student'}</Link>
            </h2>
            <p className="card__meta">
              Student ID {verification.student_number || '–'} · {verification.email || 'no email shown'}
            </p>
            <p className="card__meta">
              {(verification.program_id && programs.get(verification.program_id)) || 'Program of your faculty'} · Submitted{' '}
              {formatDateTime(verification.submitted_at)}
            </p>
          </li>
        ))}
      </ul>
      {moreError !== null && <Alert>{errorMessage(moreError)}</Alert>}
      {state.hasMore && (
        <Button variant="secondary" loading={loadingMore} onClick={loadMore}>
          Show more
        </Button>
      )}
    </>
  );
}
