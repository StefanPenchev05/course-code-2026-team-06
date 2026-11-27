import { useState } from 'react';
import { Link, Navigate, useNavigate } from 'react-router';
import { useAuth } from '../features/auth/AuthContext';
import { devPasswordLogin } from '../features/auth/devLogin';
import { RegisterForm } from '../features/auth/RegisterForm';
import { Alert } from '../shared/ui';

/**
 * With university sign-in (#68) there is nothing to register in a release
 * build, so /register just opens the login page. Written as one expression so
 * `vite build` (DEV = false) leaves the test-account form out of the bundle.
 */
export function RegisterPage() {
  return import.meta.env.DEV && devPasswordLogin() ? <TestAccountRegistration /> : <Navigate to="/login" replace />;
}

/** DEVELOPMENT ONLY: create a local test account with a Tutti password (see devLogin.ts). */
function TestAccountRegistration() {
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
      <p>
        Development test accounts only. Real users sign in with their University of Luxembourg account and don't register
        here. A coordinator from the test account's faculty approves it.
      </p>
      <RegisterForm onRegistered={handleRegistered} />
      <p>
        Already have an account? <Link to="/login">Log in</Link>
      </p>
    </section>
  );
}
