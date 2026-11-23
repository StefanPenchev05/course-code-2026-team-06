import { useEffect, useState, type FormEvent } from 'react';
import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, ConfirmAction, PageLoader, TextArea } from '../../shared/ui';
import { applicationsApi } from './api';
import { ApplicationStatusBadge } from './ApplicationStatusBadge';
import { applicantReference, formatDateTime } from './format';
import type { ApplicationReviewDetail } from './types';
import { validateRejectionReason } from './validation';

type LoadState =
  | { status: 'loading' }
  | { status: 'error'; error: unknown }
  | { status: 'ready'; application: ApplicationReviewDetail };

type Decision = 'approve' | 'reject';

function loadErrorMessage(error: unknown): string {
  // 404 = not in this coordinator's faculty (or doesn't exist); 422 = malformed ID in the URL.
  return errorMessage(error, {
    404: "This application doesn't exist or belongs to another faculty.",
    422: "This application doesn't exist or belongs to another faculty.",
  });
}

function decisionErrorMessage(error: unknown, decision: Decision): string {
  if (decision === 'approve' && error instanceof ApiError && error.status === 422 && error.message.includes('current program')) {
    return "The student's current study program no longer includes this subject, so it can't be approved. Reject it with a reason instead.";
  }
  return errorMessage(error, {
    404: 'This application is no longer available to you. The student may have moved to another faculty.',
    409: 'This application was already decided, or the student has moved to another faculty. The page now shows its current state.',
  });
}

interface DecisionPanelProps {
  onApprove: () => Promise<void>;
  onReject: (reason: string) => Promise<void>;
}

/** Approve (asks once, via ConfirmAction) or reject (with a required reason the student will see). */
function DecisionPanel({ onApprove, onReject }: DecisionPanelProps) {
  const [rejecting, setRejecting] = useState(false);
  const [reason, setReason] = useState('');
  const [reasonError, setReasonError] = useState<string | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const { formRef, signalFailure } = useFocusOnFailure();

  async function handleReject(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const error = validateRejectionReason(reason);
    setReasonError(error);
    if (error) {
      signalFailure();
      return;
    }
    setBusy(true);
    try {
      await onReject(reason.trim());
    } catch {
      // ApplicationReview already shows what went wrong; keep the reason so it can be sent again.
    } finally {
      setBusy(false);
    }
  }

  if (rejecting) {
    return (
      <form ref={formRef} noValidate className="form" onSubmit={(event) => void handleReject(event)}>
        <TextArea
          label="Reason for rejecting"
          hint="The student sees this reason in Tutti and in the decision email."
          name="rejection-reason"
          rows={4}
          autoFocus
          value={reason}
          onChange={(event) => setReason(event.target.value)}
          error={reasonError}
        />
        <div className="button-row">
          <Button type="submit" loading={busy}>
            Reject application
          </Button>
          <Button variant="secondary" disabled={busy} onClick={() => setRejecting(false)}>
            Cancel
          </Button>
        </div>
      </form>
    );
  }

  return (
    <div className="button-row">
      <ConfirmAction
        label="Approve"
        question="Approve this application? The subject is added to the student's tutor profile."
        confirmLabel="Yes, approve"
        onConfirm={onApprove}
      />
      <Button variant="link" onClick={() => setRejecting(true)}>
        Reject
      </Button>
    </div>
  );
}

/**
 * Coordinator review of one application (GET /coordinator/tutor-applications/{id}),
 * with the private grade and the decision actions. Render it with key={id}.
 */
export function ApplicationReview({ id }: { id: string }) {
  const [load, setLoad] = useState<LoadState>({ status: 'loading' });
  const [version, setVersion] = useState(0);
  const [decisionError, setDecisionError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  // The decision buttons are at the bottom; move focus up to the error so it is seen and announced.
  const { alertRef, signalFailure } = useFocusOnFailure();

  useEffect(() => {
    const controller = new AbortController();
    applicationsApi.reviewDetail(id, controller.signal).then(
      (application) => setLoad({ status: 'ready', application }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ status: 'error', error });
      },
    );
    return () => controller.abort();
  }, [id, version]);

  function retry() {
    setLoad({ status: 'loading' });
    setVersion((current) => current + 1);
  }

  /** Sends the decision. Throws on failure so the panel keeps the choice open. */
  async function decide(decision: Decision, reason: string): Promise<void> {
    setDecisionError(null);
    try {
      if (decision === 'approve') await applicationsApi.approve(id);
      else await applicationsApi.reject(id, reason);
    } catch (error) {
      setDecisionError(decisionErrorMessage(error, decision));
      signalFailure();
      // A conflict means the state changed elsewhere: show the current state.
      if (error instanceof ApiError && error.status === 409) setVersion((current) => current + 1);
      throw error;
    }
    setSuccess(
      decision === 'approve'
        ? "Application approved. The subject was added to the student's tutor profile, and they will get an email."
        : 'Application rejected. The student will get an email with your reason.',
    );
    setVersion((current) => current + 1);
  }

  if (load.status === 'loading') {
    return (
      <>
        <h1>Tutor application</h1>
        <PageLoader label="Loading application…" />
      </>
    );
  }

  if (load.status === 'error') {
    return (
      <>
        <h1>Tutor application</h1>
        <Alert
          action={
            <Button variant="secondary" onClick={retry}>
              Try again
            </Button>
          }
        >
          {loadErrorMessage(load.error)}
        </Alert>
      </>
    );
  }

  const application = load.application;

  return (
    <>
      <h1>Application to tutor {application.subject_name}</h1>
      {success && <Alert kind="success">{success}</Alert>}
      {decisionError && (
        <div ref={alertRef} tabIndex={-1}>
          <Alert>{decisionError}</Alert>
        </div>
      )}

      <dl className="details">
        <dt>Status</dt>
        <dd>
          <ApplicationStatusBadge status={application.status} />
        </dd>
        <dt>Applicant ref.</dt>
        <dd>{applicantReference(application.student_id)}</dd>
        <dt>Submitted</dt>
        <dd>{formatDateTime(application.submitted_at)}</dd>
        {application.reviewed_at && (
          <>
            <dt>Decided</dt>
            <dd>{formatDateTime(application.reviewed_at)}</dd>
          </>
        )}
        <dt>Grade</dt>
        <dd>{application.grade} / 20</dd>
        {application.rejection_reason && (
          <>
            <dt>Rejection reason</dt>
            <dd className="preserve-lines">{application.rejection_reason}</dd>
          </>
        )}
      </dl>
      <p className="muted">Grades are private. Only coordinators can see them; students see an approval badge.</p>

      <h2>Why they want to tutor this subject</h2>
      <p className="preserve-lines">{application.description}</p>

      {application.status === 'PENDING' && !success && (
        <section aria-labelledby="decision-heading">
          <h2 id="decision-heading">Decision</h2>
          <DecisionPanel onApprove={() => decide('approve', '')} onReject={(reason) => decide('reject', reason)} />
        </section>
      )}
    </>
  );
}
