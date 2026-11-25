import { Link } from 'react-router';
import { useAuth } from '../features/auth/AuthContext';
import { ResubmitForm } from '../features/auth/ResubmitForm';
import { hasRole } from '../features/auth/types';
import { Alert } from '../shared/ui';

function VerificationNotice() {
  const { user } = useAuth();
  if (!user || hasRole(user, 'coordinator')) return null;

  switch (user.verification_status) {
    case 'PENDING':
      return (
        <Alert kind="info" title="Your registration is waiting for approval">
          A coordinator from your faculty will check your student details. You can book tutors once you're approved.
        </Alert>
      );
    case 'REJECTED':
      return (
        <>
          <Alert kind="warning" title="Your registration was not approved">
            {user.rejection_reason && <p>Reason: {user.rejection_reason}</p>}
            <p>You can correct your details below and send them again.</p>
          </Alert>
          <ResubmitForm user={user} />
        </>
      );
    default:
      return null;
  }
}

export function HomePage() {
  const { user } = useAuth();
  if (!user) return null;

  return (
    <section className="page">
      <h1>Welcome, {user.name}</h1>
      <VerificationNotice />
      <ul className="link-list">
        {user.verified_student && (
          <>
            <li>
              <Link to="/tutors">Find a tutor</Link>
            </li>
            <li>
              <Link to="/sessions">My sessions</Link>
            </li>
            <li>
              <Link to="/tutor-applications">Become a tutor</Link>
            </li>
          </>
        )}
        {hasRole(user, 'tutor') && (
          <>
            <li>
              <Link to="/tutor">Teaching</Link>
            </li>
            <li>
              <Link to="/tutor/availability">My availability</Link>
            </li>
            <li>
              <Link to="/tutor/activity">My activity</Link>
            </li>
          </>
        )}
        {hasRole(user, 'coordinator') && (
          <li>
            <Link to="/coordinator/tutor-applications">Review tutor applications</Link>
          </li>
        )}
        {hasRole(user, 'coordinator') && (
          <li>
            <Link to="/coordinator">Coordinator tools</Link>
          </li>
        )}
      </ul>
    </section>
  );
}
