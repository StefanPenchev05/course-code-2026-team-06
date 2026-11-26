import { Link, useParams } from 'react-router';
import { VerificationReview } from '../features/verifications';

/** /coordinator/verifications/:id (#51): one registration with approve and reject. */
export function CoordinatorVerificationPage() {
  const { id = '' } = useParams();

  return (
    <section className="page">
      <p>
        <Link to="/coordinator/verifications">← All registrations</Link>
      </p>
      <VerificationReview key={id} id={id} />
    </section>
  );
}
