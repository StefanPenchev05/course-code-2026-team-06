import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Link } from 'react-router';
import { ProgramPicker } from '../catalog';
import { Alert, Button, TextField } from '../../shared/ui';
import { authApi } from './api';
import { LIMITS, registrationProblem, validateRegistration, type RegistrationErrors, type RegistrationForm } from './registration';

const EMPTY: RegistrationForm = {
  name: '',
  email: '',
  studentNumber: '',
  facultyId: '',
  programId: '',
  password: '',
  passwordConfirm: '',
};

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
  // Bumped after every failed submit so focus moves to the problem, which may be scrolled out of view.
  const [failedAttempts, setFailedAttempts] = useState(0);
  const formRef = useRef<HTMLFormElement>(null);
  const alertRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (failedAttempts === 0) return;
    const target = alertRef.current ?? formRef.current?.querySelector<HTMLElement>('[aria-invalid="true"]');
    target?.focus();
  }, [failedAttempts]);

  function update<K extends keyof RegistrationForm>(field: K, value: RegistrationForm[K]) {
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
    if (Object.values(found).some(Boolean)) {
      setFailedAttempts((n) => n + 1);
      return;
    }

    setSubmitting(true);
    const email = form.email.trim();
    try {
      await authApi.register({
        name: form.name.trim(),
        email,
        student_number: form.studentNumber.trim(),
        program_id: form.programId,
        password: form.password,
      });
    } catch (error) {
      const problem = registrationProblem(error);
      setErrors(problem.fields);
      setFormError(problem.form);
      setAlreadyRegistered(problem.alreadyRegistered);
      // Clear the vanished program without update(), which would also clear its error message.
      if (problem.fields.programId) setForm((previous) => ({ ...previous, programId: '' }));
      setSubmitting(false);
      setFailedAttempts((n) => n + 1);
      return;
    }
    await onRegistered(email, form.password);
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
      <TextField
        label="Full name"
        name="name"
        autoComplete="name"
        value={form.name}
        onChange={(event) => update('name', event.target.value)}
        error={errors.name}
        maxLength={LIMITS.name}
      />
      <TextField
        label="University email"
        type="email"
        name="email"
        autoComplete="email"
        hint="Use the email address your university gave you."
        value={form.email}
        onChange={(event) => update('email', event.target.value)}
        error={errors.email}
        maxLength={LIMITS.email}
      />
      <TextField
        label="Student number"
        name="student_number"
        autoComplete="off"
        hint="As shown on your student card. A coordinator checks it before you can book tutors."
        value={form.studentNumber}
        onChange={(event) => update('studentNumber', event.target.value)}
        error={errors.studentNumber}
        maxLength={LIMITS.studentNumber}
      />
      <ProgramPicker
        facultyId={form.facultyId}
        programId={form.programId}
        onFacultyChange={(value) => update('facultyId', value)}
        onProgramChange={(value) => update('programId', value)}
        facultyError={errors.facultyId}
        programError={errors.programId}
        disabled={submitting}
      />
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
