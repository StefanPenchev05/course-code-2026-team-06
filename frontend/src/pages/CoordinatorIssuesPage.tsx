import { useSearchParams } from 'react-router';
import { ISSUE_STATUSES, IssueInbox, isIssueStatus, type IssueStatus } from '../features/issueInbox';
import { SelectField } from '../shared/ui';

const FILTER_LABELS: Record<IssueStatus, string> = { OPEN: 'Open', RESOLVED: 'Resolved' };

/** /coordinator/issues (#53): problem reports for the coordinator's faculty. The filter lives in the URL (?status=). */
export function CoordinatorIssuesPage() {
  const [params, setParams] = useSearchParams();
  const requested = (params.get('status') ?? '').toUpperCase();
  const status: IssueStatus = isIssueStatus(requested) ? requested : 'OPEN';

  return (
    <section className="page">
      <h1>Problem reports</h1>
      <p>
        General problems that students and tutors reported to your faculty: about a session of one of your faculty’s tutors, or
        from a student of your faculty. No-show reports are separate.
      </p>
      <div className="filters">
        <SelectField
          label="Show reports"
          value={status}
          onChange={(event) => setParams(event.target.value === 'OPEN' ? {} : { status: event.target.value.toLowerCase() }, { replace: true })}
        >
          {ISSUE_STATUSES.map((option) => (
            <option key={option} value={option}>
              {FILTER_LABELS[option]}
            </option>
          ))}
        </SelectField>
      </div>
      <IssueInbox key={status} status={status} />
    </section>
  );
}
