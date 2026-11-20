import { useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { useAuth } from '../features/auth/AuthContext';
import { safeRedirect } from '../features/auth/redirect';
import { errorMessage } from '../shared/api/errors';
import { Alert, Button, TextField } from '../shared/ui';

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

// Temporary email/password login. It will be replaced by Uni.lu SSO (#68).
export function LoginPage() {
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
          403: import.meta.env.DEV
            ? "This account can't log in right now (it may be suspended or closed). If you're developing locally, also check that you opened http://localhost:5173 and set PUBLIC_ORIGIN=http://localhost:5173 in the backend .env."
            : "This account can't log in right now. If it was suspended or closed, contact your faculty coordinator.",
        }),
      );
      setSubmitting(false);
    }
  }

  return (
    <section className="page page--narrow">
      <h1>Log in</h1>
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
          type="password"
          name="password"
          autoComplete="current-password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          error={fieldErrors.password}
        />
        <Button type="submit" loading={submitting}>
          Log in
        </Button>
      </form>
      <p>
        New to Tutti? <Link to="/register">Create an account</Link>
      </p>
    </section>
  );
}
