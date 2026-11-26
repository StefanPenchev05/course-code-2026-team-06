import { useSearchParams } from 'react-router';
import { ReportForm } from '../features/issueReports';

/** /report (#52): a general problem report, optionally about one session (?session=<id>). */
export function ReportProblemPage() {
  const [params] = useSearchParams();
  const sessionId = params.get('session');

  return (
    <section className="page">
      <h1>Report a problem</h1>
      <p>Tell the faculty coordinators about a problem with tutoring, for example with a session, a tutor or a student.</p>
      <p className="muted">
        This is not a no-show report. A problem report doesn’t change a session’s outcome and doesn’t count as a missed session
        for anyone.
      </p>
      <ReportForm key={sessionId ?? 'general'} sessionId={sessionId} />
    </section>
  );
}
