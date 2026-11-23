import { useState, type FormEvent } from 'react';
import { Link } from 'react-router';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, TextField } from '../../shared/ui';
import { authApi } from './api';
import {
  EMPTY_DETAILS,
  hasErrors,
  LIMITS,
  studentDetailsProblem,
  toStudentDetails,
  validateRegistration,
  type RegistrationErrors,
  type RegistrationForm,
} from './registration';
import { StudentDetailsFields } from './StudentDetailsFields';

const EMPTY: RegistrationForm = { ...EMPTY_DETAILS, password: '', passwordConfirm: '' };

interface RegisterFormProps {
  /** Called after the account was created. Receives the credentials so the page can log the student in. */
  onRegistered: (email: string, password: string) => Promise<void> | void;
}

// Temporary email/password registration. It will be replaced by Uni.lu SSO (#68);
// enrollment verification by a coordinator stays either way.
export function RegisterForm({ onRegistered }: RegisterFormProps) {
  const [form, setForm] = useState<RegistrationForm>(EMPTY);
  const [errors, setErrors] = useState<RegistrationErrors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [alreadyRegistered, setAlreadyRegistered] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  function update(field: keyof RegistrationForm, value: string) {
    setForm((previous) => ({ ...previous, [field]: value }));
    // Clear the message for a field as soon as the student changes it.
    setErrors((previous) => (previous[field] ? { ...previous, [field]: undefined } : previous));
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;
    const found = validateRegistration(form);
    setErrors(found);
    setFormError(null);
    setAlreadyRegistered(false);
    if (hasErrors(found)) {
      signalFailure();
      return;
    }

    setSubmitting(true);
    const details = toStudentDetails(form);
    try {
      await authApi.register({ ...details, password: form.password });
    } catch (error) {
      const problem = studentDetailsProblem(error, 'register');
      setErrors(problem.fields);
      setFormError(problem.form);
      setAlreadyRegistered(problem.alreadyRegistered);
      // Clear the vanished program without update(), which would also clear its error message.
      if (problem.fields.programId) setForm((previous) => ({ ...previous, programId: '' }));
      setSubmitting(false);
      signalFailure();
      return;
    }
    await onRegistered(details.email, form.password);
    setSubmitting(false);
  }

  return (
    <form ref={formRef} noValidate onSubmit={(event) => void handleSubmit(event)} className="form">
      {formError && (
        <div ref={alertRef} tabIndex={-1}>
          <Alert>
            <p>{formError}</p>
            {alreadyRegistered && (
              <p>
                <Link to="/login">Log in instead</Link>
              </p>
            )}
          </Alert>
        </div>
      )}
      <StudentDetailsFields values={form} errors={errors} onChange={update} disabled={submitting} />
      <TextField
        label="Password"
        type="password"
        name="password"
        autoComplete="new-password"
        hint={`At least ${LIMITS.passwordMin} characters.`}
        value={form.password}
        onChange={(event) => update('password', event.target.value)}
        error={errors.password}
      />
      <TextField
        label="Repeat password"
        type="password"
        name="password_confirm"
        autoComplete="new-password"
        value={form.passwordConfirm}
        onChange={(event) => update('passwordConfirm', event.target.value)}
        error={errors.passwordConfirm}
      />
      <Button type="submit" loading={submitting}>
        Create account
      </Button>
    </form>
  );
}
