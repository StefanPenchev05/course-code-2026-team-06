import { useSearchParams } from 'react-router';
import {
  APPLICATION_STATUSES,
  ApplicationQueue,
  isApplicationStatus,
  type ApplicationStatus,
} from '../features/tutorApplications';
import { SelectField } from '../shared/ui';

const FILTER_LABELS: Record<ApplicationStatus, string> = {
  PENDING: 'Waiting for review',
  APPROVED: 'Approved',
  REJECTED: 'Rejected',
};

/** /coordinator/tutor-applications: faculty-scoped review queue (#30). The filter lives in the URL (?status=). */
export function CoordinatorTutorApplicationsPage() {
  const [params, setParams] = useSearchParams();
  const requested = params.get('status') ?? '';
  const status: ApplicationStatus = isApplicationStatus(requested) ? requested : 'PENDING';

  return (
    <section className="page">
      <h1>Tutor applications</h1>
      <p>Applications from students in your faculty. Approving one adds the subject to the student's tutor profile.</p>
      <SelectField
        label="Show applications"
        value={status}
        onChange={(event) =>
          setParams(event.target.value === 'PENDING' ? {} : { status: event.target.value }, { replace: true })
        }
      >
        {APPLICATION_STATUSES.map((option) => (
          <option key={option} value={option}>
            {FILTER_LABELS[option]}
          </option>
        ))}
      </SelectField>
      <ApplicationQueue key={status} status={status} />
    </section>
  );
}
