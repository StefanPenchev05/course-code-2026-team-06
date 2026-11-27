import { Link, useParams } from 'react-router';
import { AccountReview } from '../features/accountDiscipline';

/** /coordinator/users/:id (#56): suspension state, suspend or reinstate, and the account's history. */
export function CoordinatorUserPage() {
  const { id = '' } = useParams();

  return (
    <section className="page">
      <p>
        <Link to="/coordinator/flags">← Flagged accounts</Link>
      </p>
      <AccountReview key={id} userId={id} />
    </section>
  );
}
