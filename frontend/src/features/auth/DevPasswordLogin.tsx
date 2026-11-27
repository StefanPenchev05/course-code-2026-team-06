import { useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { Alert, Button, TextField } from '../../shared/ui';
import { useAuth } from './AuthContext';
import { safeRedirect } from './redirect';

interface FieldErrors {
  email?: string;
  password?: string;
}

function validate(email: string, password: string): FieldErrors {
  const errors: FieldErrors = {};
  if (!email.trim()) errors.email = 'Enter your university email.';
  else if (!/^[^\s@]+@[^\s@]+$/.test(email.trim())) errors.email = 'Enter a valid email address.';
  if (!password) errors.password = 'Enter your password.';
  return errors;
}

/**
 * DEVELOPMENT ONLY: log in to a local Tutti test account (legacy POST /auth/login).
 * Never rendered in a release build; see devLogin.ts for why it still exists.
 */
export function DevPasswordLogin({ onSubmit }: { onSubmit?: () => void }) {
  const { login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const errors = validate(email, password);
    setFieldErrors(errors);
    setFormError(null);
    onSubmit?.();
    if (errors.email || errors.password) return;

    setSubmitting(true);
    try {
      await login(email.trim(), password);
      navigate(safeRedirect(location.state), { replace: true });
    } catch (error) {
      setFormError(
        errorMessage(error, {
          401: 'Email or password is incorrect.',
          // The API answers 403 both for a suspended/closed account and for a
          // wrong Origin header; the response doesn't say which.
          403: "This account can't log in right now (it may be suspended or closed). If you're developing locally, also check that you opened http://localhost:5173 and set PUBLIC_ORIGIN=http://localhost:5173 in the backend .env.",
        }),
      );
      setSubmitting(false);
    }
  }

  return (
    <div className="dev-login">
      <h2>Development test accounts</h2>
      <p>
        Only in development builds, for the local demo and the automated tests. Use a local Tutti test account here, never
        your university password. Release builds don't include this form.
      </p>
      <form noValidate onSubmit={(event) => void handleSubmit(event)} className="form">
        {formError && <Alert>{formError}</Alert>}
        <TextField
          label="University email"
          type="email"
          name="email"
          autoComplete="username"
          value={email}
          onChange={(event) => setEmail(event.target.value)}
          error={fieldErrors.email}
        />
        <TextField
          label="Password"
          hint="The test account's Tutti password."
          type="password"
          name="password"
          autoComplete="current-password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          error={fieldErrors.password}
        />
        <Button type="submit" variant="secondary" loading={submitting}>
          Log in
        </Button>
      </form>
      <p>
        Need a test account? <Link to="/register">Create an account</Link> or use the seeded ones in docs/local-demo.md.
      </p>
    </div>
  );
}
