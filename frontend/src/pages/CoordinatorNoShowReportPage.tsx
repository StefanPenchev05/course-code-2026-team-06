import { Link, useParams } from 'react-router';
import { NoShowReview } from '../features/noShowReports';

/** /coordinator/no-show-reports/:id (#54): one no-show allegation with the decision. */
export function CoordinatorNoShowReportPage() {
  const { id = '' } = useParams();

  return (
    <section className="page">
      <p>
        <Link to="/coordinator/no-show-reports">← All no-show reports</Link>
      </p>
      <NoShowReview key={id} id={id} />
    </section>
  );
}
