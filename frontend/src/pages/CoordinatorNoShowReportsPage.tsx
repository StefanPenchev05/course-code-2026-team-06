import { useSearchParams } from 'react-router';
import { isNoShowStatus, NO_SHOW_STATUSES, NoShowList, type NoShowStatus } from '../features/noShowReports';
import { SelectField } from '../shared/ui';

const FILTER_LABELS: Record<NoShowStatus, string> = { PENDING: 'Waiting for a decision', CONFIRMED: 'Confirmed', REJECTED: 'Rejected' };

/** /coordinator/no-show-reports (#54): no-show allegations for sessions taught in the faculty. The filter lives in the URL. */
export function CoordinatorNoShowReportsPage() {
  const [params, setParams] = useSearchParams();
  const requested = (params.get('status') ?? '').toUpperCase();
  const status: NoShowStatus = isNoShowStatus(requested) ? requested : 'PENDING';

  return (
    <section className="page">
      <h1>No-show reports</h1>
      <p>
        Participants of sessions taught in your faculty can report that the other person didn’t show up, up to seven days after the
        session. Each report is an allegation that you confirm or reject. General problem reports are separate.
      </p>
      <div className="filters">
        <SelectField
          label="Show reports"
          value={status}
          onChange={(event) => setParams(event.target.value === 'PENDING' ? {} : { status: event.target.value.toLowerCase() }, { replace: true })}
        >
          {NO_SHOW_STATUSES.map((option) => (
            <option key={option} value={option}>
              {FILTER_LABELS[option]}
            </option>
          ))}
        </SelectField>
      </div>
      <NoShowList key={status} status={status} />
    </section>
  );
}
