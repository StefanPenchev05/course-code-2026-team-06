import { Link } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { usePagedList } from '../../shared/hooks/usePagedList';
import { useLiveRefresh } from '../../shared/realtime';
import { Alert, Button, LiveAnnouncement, Spinner } from '../../shared/ui';
import { disciplineApi } from './api';
import { formatDateTime, reference } from './rules';
import { SuspensionBadge } from './SuspensionBadge';
import type { FlaggedUser } from './types';

const flagId = (flag: FlaggedUser) => flag.flag_id;

/** Semester flags stored for the coordinator's faculty, newest first (FR18, UC16). */
export function FlaggedList() {
  const { state, reload, refresh, change, loadMore, loadingMore, moreError } = usePagedList(disciplineApi.flagged, flagId);
  // New flags and suspension decisions arrive as coordination.changed; refresh in place,
  // keeping "Show more" results (#93).
  useLiveRefresh(['coordination.changed'], refresh);

  if (state.status === 'loading') return <Spinner label="Loading flagged accounts…" />;
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
  const announcement = <LiveAnnouncement change={change} noun={['flag', 'flags']} />;
  if (state.items.length === 0) {
    return (
      <>
        {announcement}
        <p>No accounts of your faculty are flagged.</p>
      </>
    );
  }

  return (
    <>
      {announcement}
      <ul className="card-list" aria-label="Flagged accounts">
        {state.items.map((flag) => (
          <li key={flag.flag_id} className="card">
            <div className="session__head">
              <h2 className="tutor-card__name">
                <Link to={`/coordinator/users/${flag.user_id}`}>Account {reference(flag.user_id)}</Link>
              </h2>
              <SuspensionBadge suspended={flag.is_suspended} />
            </div>
            <p className="card__meta">
              {flag.strike_count} {flag.strike_count === 1 ? 'violation' : 'violations'} in semester {reference(flag.semester_id)} ·
              Flagged {formatDateTime(flag.flagged_at)}
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
