import { Link } from 'react-router';
import { FacultyOverview } from '../features/coordinatorMonitoring';

/** /coordinator (#35): faculty tutoring totals and the coordinator's tools. */
export function CoordinatorHomePage() {
  return (
    <section className="page">
      <h1>Coordinator</h1>
      <p>Tutoring in your faculty at a glance.</p>
      <FacultyOverview />
      <h2>Tools</h2>
      <ul>
        <li>
          <Link to="/coordinator/verifications">Student registrations</Link>: approve or reject new students of your faculty.
        </li>
        <li>
          <Link to="/coordinator/tutors">Tutors</Link>: activity, schedules and feedback of each tutor; cancel sessions for policy
          reasons.
        </li>
        <li>
          <Link to="/coordinator/tutor-applications">Tutor applications</Link>: approve or reject students who want to tutor.
        </li>
      </ul>
    </section>
  );
}
