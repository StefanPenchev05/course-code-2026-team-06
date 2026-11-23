import { useState } from 'react';
import { Link, useNavigate } from 'react-router';
import { useAuth } from '../features/auth/AuthContext';
import { RegisterForm } from '../features/auth/RegisterForm';
import { Alert } from '../shared/ui';

export function RegisterPage() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const [needsLogin, setNeedsLogin] = useState(false);

  // Registering doesn't start a session, so log the new student in right away.
  // The home page then shows "waiting for approval".
  async function handleRegistered(email: string, password: string) {
    try {
      await login(email, password);
      navigate('/', { replace: true });
    } catch {
      setNeedsLogin(true);
    }
  }

  if (needsLogin) {
    return (
      <section className="page page--narrow">
        <h1>Your account was created</h1>
        <Alert kind="success" title="Registration sent for approval">
          A coordinator from your faculty will check your details.
        </Alert>
        <p>
          <Link to="/login">Log in to see your status</Link>
        </p>
      </section>
    );
  }

  return (
    <section className="page page--narrow">
      <h1>Create an account</h1>
      <p>For students of the university. A coordinator from your faculty approves new accounts.</p>
      <RegisterForm onRegistered={handleRegistered} />
      <p>
        Already have an account? <Link to="/login">Log in</Link>
      </p>
    </section>
  );
}
