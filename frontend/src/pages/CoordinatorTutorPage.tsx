import { Link, useParams } from 'react-router';
import { TutorOverview } from '../features/coordinatorMonitoring';

/** /coordinator/tutors/:id (#35): one tutor's totals, subjects, schedule with policy cancellation, and feedback. */
export function CoordinatorTutorPage() {
  const { id = '' } = useParams();

  return (
    <section className="page">
      <p>
        <Link to="/coordinator/tutors">← All tutors</Link>
      </p>
      <TutorOverview key={id} id={id} />
    </section>
  );
}
