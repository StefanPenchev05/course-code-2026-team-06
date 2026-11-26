import { Link, useParams } from 'react-router';
import { IssueReview } from '../features/issueInbox';

/** /coordinator/issues/:id (#53): one problem report with its resolution. */
export function CoordinatorIssuePage() {
  const { id = '' } = useParams();

  return (
    <section className="page">
      <p>
        <Link to="/coordinator/issues">← All problem reports</Link>
      </p>
      <IssueReview key={id} id={id} />
    </section>
  );
}
