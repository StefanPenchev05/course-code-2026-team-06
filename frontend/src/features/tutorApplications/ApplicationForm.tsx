import { useState, type FormEvent } from 'react';
import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, TextArea, TextField } from '../../shared/ui';
import { useAuth } from '../auth/AuthContext';
import { SubjectSearch, type Subject } from '../catalog';
import { applicationsApi } from './api';
import { parseGrade, validateApplication, type ApplicationFieldErrors } from './validation';

function submitErrorMessage(error: unknown): string {
  // domain.ErrIneligibleSubject: "invalid input: subject is not in the student's current program".
  if (error instanceof ApiError && error.status === 422 && error.message.includes('current program')) {
    return "This subject isn't part of your current study program, so you can't apply for it. Choose another subject.";
  }
  return errorMessage(error, {
    403: "You can't apply right now. Only verified students with an active account can apply.",
    409: "You can't apply for this subject right now. You may already tutor it, or another application of yours is still waiting for review.",
  });
}

/**
 * Apply to tutor one subject (POST /tutor-applications). The student picks a
 * subject of their own program (SubjectSearch from the catalog feature), then
 * gives the grade and a short description.
 */
export function ApplicationForm({ onSubmitted }: { onSubmitted: (subject: Subject) => void }) {
  const { user } = useAuth();
  const [subject, setSubject] = useState<Subject | null>(null);
  const [grade, setGrade] = useState('');
  const [description, setDescription] = useState('');
  const [fieldErrors, setFieldErrors] = useState<ApplicationFieldErrors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  function chooseSubject(chosen: Subject | null) {
    setSubject(chosen);
    setFormError(null);
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!subject || submitting) return;

    const errors = validateApplication(grade, description);
    setFieldErrors(errors);
    setFormError(null);
    const parsedGrade = parseGrade(grade);
    if (errors.grade || errors.description || parsedGrade === null) {
      signalFailure();
      return;
    }

    setSubmitting(true);
    try {
      await applicationsApi.submit({ subject_id: subject.id, grade: parsedGrade, description: description.trim() });
      onSubmitted(subject);
    } catch (error) {
      // Keep everything the student typed so they can fix it and try again.
      setFormError(submitErrorMessage(error));
      setSubmitting(false);
      signalFailure();
    }
  }

  return (
    <>
      <SubjectSearch selected={subject} onSelect={chooseSubject} programId={user?.program_id} />
      {subject && (
        <form ref={formRef} noValidate className="form" onSubmit={(event) => void handleSubmit(event)}>
          {formError && (
            <div ref={alertRef} tabIndex={-1}>
              <Alert>{formError}</Alert>
            </div>
          )}
          <TextField
            label="Your grade in this subject (out of 20)"
            hint="You need at least 15. Up to two decimals, for example 17.5. Only your faculty coordinator sees your grade."
            name="grade"
            inputMode="decimal"
            autoComplete="off"
            autoFocus
            value={grade}
            onChange={(event) => setGrade(event.target.value)}
            error={fieldErrors.grade}
          />
          <TextArea
            label="Why do you want to tutor this subject?"
            hint="Your coordinator reads this when reviewing your application."
            name="description"
            rows={5}
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            error={fieldErrors.description}
          />
          <div>
            <Button type="submit" loading={submitting}>
              Send application
            </Button>
          </div>
        </form>
      )}
    </>
  );
}
