import { Link, useParams } from 'react-router';
import { ApplicationReview } from '../features/tutorApplications';

/** /coordinator/tutor-applications/:id: one application with the private grade, approve and reject (#30). */
export function CoordinatorTutorApplicationPage() {
  const { id = '' } = useParams();

  return (
    <section className="page">
      <p>
        <Link to="/coordinator/tutor-applications">← All tutor applications</Link>
      </p>
      <ApplicationReview key={id} id={id} />
    </section>
  );
}
