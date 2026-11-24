import { Link } from 'react-router';
import { TutorQueues } from '../features/bookings/TutorQueues';

/** Teaching (#33): answer session requests and complete confirmed sessions. */
export function TeachingPage() {
  return (
    <section className="page">
      <h1>Teaching</h1>
      <p>
        Students can book you during your <Link to="/tutor/availability">free times</Link>. Answer requests before their deadline, and mark
        sessions as completed once they have taken place.
      </p>
      <TutorQueues />
    </section>
  );
}
