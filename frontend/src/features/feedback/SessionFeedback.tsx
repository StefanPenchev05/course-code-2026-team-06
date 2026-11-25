import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, Spinner, TextArea } from '../../shared/ui';
import { feedbackApi } from './api';
import { RatingField, RatingStars } from './Rating';
import type { FeedbackEntry } from './types';
import { characterCount, COMMENT_MAX_CHARS, feedbackError, toFeedbackInput, validateFeedback, type FeedbackErrors } from './validation';

interface SessionFeedbackProps {
  sessionId: string;
  subjectName: string;
  /** The viewer's role in the session: only the student can rate it. */
  viewerRole: 'STUDENT' | 'TUTOR';
}

type Load = { attempt: number; status: 'ready'; entry: FeedbackEntry | null } | { attempt: number; status: 'error'; message: string };

/**
 * The feedback part of a COMPLETED session card. The student sees their rating, or can rate the
 * session once; the tutor sees the student's rating. The API decides who may read or write it.
 */
export function SessionFeedback({ sessionId, subjectName, viewerRole }: SessionFeedbackProps) {
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<Load | null>(null);
  const [writing, setWriting] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    feedbackApi.forSession(sessionId, controller.signal).then(
      (entry) => setLoad({ attempt, status: 'ready', entry }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ attempt, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [sessionId, attempt]);

  const current = load?.attempt === attempt ? load : null;
  const isStudent = viewerRole === 'STUDENT';

  let content: ReactNode;
  if (!current) {
    content = <Spinner label="Loading feedback…" />;
  } else if (current.status === 'error') {
    content = (
      <Alert
        title="Feedback could not be loaded"
        action={
          <Button variant="secondary" onClick={() => setAttempt((n) => n + 1)}>
            Try again
          </Button>
        }
      >
        {current.message}
      </Alert>
    );
  } else if (current.entry) {
    content = (
      <>
        {notice && <Alert kind="success">{notice}</Alert>}
        <p>
          {isStudent ? 'Your rating' : 'The student’s rating'}: <RatingStars rating={current.entry.rating} />
        </p>
        {current.entry.comment && <p className="preserve-lines feedback__comment">{current.entry.comment}</p>}
      </>
    );
  } else if (!isStudent) {
    content = <p className="muted">The student hasn’t rated this session yet.</p>;
  } else if (writing) {
    content = (
      <FeedbackForm
        sessionId={sessionId}
        onCancel={() => setWriting(false)}
        onSent={(entry) => {
          setWriting(false);
          setNotice('Thank you! Your feedback was sent to the tutor.');
          setLoad({ attempt, status: 'ready', entry });
        }}
        onAlreadyRated={() => {
          setWriting(false);
          setNotice('You had already rated this session.');
          setAttempt((n) => n + 1);
        }}
      />
    );
  } else {
    content = (
      <>
        <p className="muted">You haven’t rated this session yet.</p>
        <Button variant="secondary" onClick={() => setWriting(true)} aria-label={`Rate the ${subjectName} session`}>
          Rate this session
        </Button>
      </>
    );
  }

  return (
    <div className="feedback">{content}</div>
  );
}

interface FeedbackFormProps {
  sessionId: string;
  onCancel: () => void;
  onSent: (entry: FeedbackEntry) => void;
  onAlreadyRated: () => void;
}

function FeedbackForm({ sessionId, onCancel, onSent, onAlreadyRated }: FeedbackFormProps) {
  const [rating, setRating] = useState<number | null>(null);
  const [comment, setComment] = useState('');
  const [errors, setErrors] = useState<FeedbackErrors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const problems = validateFeedback(rating, comment);
    setErrors(problems);
    setFormError(null);
    if (rating === null || problems.rating || problems.comment) {
      signalFailure();
      return;
    }
    setSubmitting(true);
    try {
      onSent(await feedbackApi.submit(sessionId, toFeedbackInput(rating, comment)));
    } catch (error) {
      setSubmitting(false);
      if (error instanceof ApiError && error.status === 409) {
        onAlreadyRated();
        return;
      }
      setFormError(feedbackError(error));
      signalFailure();
    }
  }

  return (
    <form ref={formRef} noValidate onSubmit={(event) => void submit(event)} className="form" aria-label="Rate this session">
      {formError && (
        <div ref={alertRef} tabIndex={-1}>
          <Alert>{formError}</Alert>
        </div>
      )}
      <RatingField
        value={rating}
        onChange={(value) => {
          setRating(value);
          setErrors((previous) => ({ ...previous, rating: undefined }));
        }}
        error={errors.rating}
      />
      <TextArea
        label="Comment (optional)"
        name="comment"
        rows={4}
        value={comment}
        onChange={(event) => {
          setComment(event.target.value);
          setErrors((previous) => ({ ...previous, comment: undefined }));
        }}
        error={errors.comment}
        hint={`Your tutor and your faculty coordinator can read it. ${characterCount(comment.trim())}/${COMMENT_MAX_CHARS} characters.`}
      />
      <p className="muted">You can rate a session only once, and you can’t change your feedback afterwards.</p>
      <div className="button-row">
        <Button type="submit" loading={submitting}>
          Send feedback
        </Button>
        <Button variant="link" onClick={onCancel} disabled={submitting}>
          Not now
        </Button>
      </div>
    </form>
  );
}
