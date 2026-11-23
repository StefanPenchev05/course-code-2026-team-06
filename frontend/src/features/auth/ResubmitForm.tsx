import { useState, type FormEvent } from 'react';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button } from '../../shared/ui';
import { authApi } from './api';
import { useAuth } from './AuthContext';
import {
  EMPTY_DETAILS,
  hasErrors,
  studentDetailsProblem,
  toStudentDetails,
  validateStudentDetails,
  type FieldErrors,
  type StudentDetailsForm,
} from './registration';
import { StudentDetailsFields } from './StudentDetailsFields';
import type { CurrentUser } from './types';

/**
 * Lets a student whose registration was rejected correct their details and
 * send them for a new check (POST /auth/resubmit-verification).
 * The API only returns name and email for the current user, so those are
 * pre-filled; the student number and program must be entered again.
 */
export function ResubmitForm({ user }: { user: CurrentUser }) {
  const { refresh } = useAuth();
  const [form, setForm] = useState<StudentDetailsForm>({ ...EMPTY_DETAILS, name: user.name, email: user.email });
  const [errors, setErrors] = useState<FieldErrors<StudentDetailsForm>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [sent, setSent] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  function update(field: keyof StudentDetailsForm, value: string) {
    setForm((previous) => ({ ...previous, [field]: value }));
    setErrors((previous) => (previous[field] ? { ...previous, [field]: undefined } : previous));
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;
    const found = validateStudentDetails(form);
    setErrors(found);
    setFormError(null);
    if (hasErrors(found)) {
      signalFailure();
      return;
    }

    setSubmitting(true);
    try {
      await authApi.resubmitVerification(toStudentDetails(form));
    } catch (error) {
      const problem = studentDetailsProblem(error, 'resubmit');
      setErrors(problem.fields);
      setFormError(problem.form);
      if (problem.fields.programId) setForm((previous) => ({ ...previous, programId: '' }));
      setSubmitting(false);
      signalFailure();
      return;
    }
    // Show success even if reloading the status fails; resubmitting again would only get a 409.
    setSent(true);
    // Reload the session from the server: the status is now PENDING, and the
    // home page replaces this form with the "waiting for approval" notice.
    await refresh();
  }

  if (sent) {
    return (
      <Alert kind="success" title="Your details were sent again">
        A coordinator from your faculty will check them.
      </Alert>
    );
  }

  const emailChanged = form.email.trim().toLowerCase() !== user.email.toLowerCase();

  return (
    <form ref={formRef} noValidate onSubmit={(event) => void handleSubmit(event)} className="form" aria-labelledby="resubmit-heading">
      <h2 id="resubmit-heading">Correct your details</h2>
      <p>Check what the coordinator mentioned, fix your details and send them again.</p>
      {formError && (
        <div ref={alertRef} tabIndex={-1}>
          <Alert>{formError}</Alert>
        </div>
      )}
      <StudentDetailsFields
        values={form}
        errors={errors}
        onChange={update}
        disabled={submitting}
        emailHint={
          emailChanged
            ? 'You will log in with this new email address from now on.'
            : 'Use the email address your university gave you.'
        }
      />
      <Button type="submit" loading={submitting}>
        Send for approval again
      </Button>
    </form>
  );
}
