import { errorMessage } from '../../shared/api/errors';
import { Alert, Button } from '../../shared/ui';
import { ApplicationStatusBadge } from './ApplicationStatusBadge';
import { formatDateTime } from './format';
import type { TutorApplication } from './types';

interface MyApplicationsProps {
  applications: TutorApplication[];
  hasMore: boolean;
  loadingMore: boolean;
  moreError: unknown;
  onLoadMore: () => void;
}

/** The student's own applications with status and, if rejected, the coordinator's reason. Grades are never shown here. */
export function MyApplications({ applications, hasMore, loadingMore, moreError, onLoadMore }: MyApplicationsProps) {
  if (applications.length === 0) return <p>You haven't applied to tutor a subject yet.</p>;

  return (
    <>
      <ul className="card-list">
        {applications.map((application) => (
          <li key={application.id} className="card">
            <div className="application__head">
              <h3>{application.subject_name}</h3>
              <ApplicationStatusBadge status={application.status} />
            </div>
            <dl className="details">
              <dt>Submitted</dt>
              <dd>{formatDateTime(application.submitted_at)}</dd>
              {application.reviewed_at && (
                <>
                  <dt>Decided</dt>
                  <dd>{formatDateTime(application.reviewed_at)}</dd>
                </>
              )}
              {application.status === 'REJECTED' && application.rejection_reason && (
                <>
                  <dt>Reason</dt>
                  <dd className="preserve-lines">{application.rejection_reason}</dd>
                </>
              )}
            </dl>
            {application.status === 'APPROVED' && <p>This subject is now on your tutor profile.</p>}
            {application.status === 'REJECTED' && <p>You can send a new application if your situation changes.</p>}
          </li>
        ))}
      </ul>
      {moreError !== null && <Alert>{errorMessage(moreError)}</Alert>}
      {hasMore && (
        <Button variant="secondary" loading={loadingMore} onClick={onLoadMore}>
          Show more applications
        </Button>
      )}
    </>
  );
}
