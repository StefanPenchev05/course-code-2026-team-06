import { useEffect, useState, type FormEvent } from 'react';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, ConfirmAction, PageLoader, TextArea } from '../../shared/ui';
import { verificationsApi } from './api';
import { decisionError, formatDateTime, isConflict, loadError, reasonProblem } from './rules';
import type { Verification } from './types';
import { useProgramNames } from './useProgramNames';
import { VerificationStatusBadge } from './VerificationStatusBadge';

type Load = { version: number; status: 'ready'; verification: Verification } | { version: number; status: 'error'; error: unknown };

/**
 * One student registration of the coordinator's faculty, with approve and reject (FR2, UC01).
 * Render it with key={id}.
 */
export function VerificationReview({ id }: { id: string }) {
  const [version, setVersion] = useState(0);
  const [load, setLoad] = useState<Load | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const programs = useProgramNames();
  // The decision buttons are at the bottom; move focus up to the error so it is seen and announced.
  const { alertRef, signalFailure } = useFocusOnFailure();

  useEffect(() => {
    const controller = new AbortController();
    verificationsApi.detail(id, controller.signal).then(
      (verification) => setLoad({ version, status: 'ready', verification }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ version, status: 'error', error });
      },
    );
    return () => controller.abort();
  }, [id, version]);

  /** Sends the decision. Throws on failure so the panel keeps the choice open. */
  async function decide(reason: string | null): Promise<void> {
    setFailure(null);
    try {
      if (reason === null) await verificationsApi.approve(id);
      else await verificationsApi.reject(id, reason);
    } catch (error) {
      setFailure(decisionError(error));
      signalFailure();
      // A conflict means someone else decided: show the current state.
      if (isConflict(error)) setVersion((n) => n + 1);
      throw error;
    }
    setSuccess(
      reason === null
        ? 'Registration approved. The student can now use Tutti.'
        : 'Registration rejected. The student sees your reason when they log in and can correct their details.',
    );
    setVersion((n) => n + 1);
  }

  // While reloading after a decision, keep showing the last loaded state.
  const current = load;
  if (!current || (current.version !== version && current.status === 'error')) {
    return (
      <>
        <h1>Student registration</h1>
        <PageLoader label="Loading registration…" />
      </>
    );
  }
  if (current.status === 'error') {
    return (
      <>
        <h1>Student registration</h1>
        <Alert
          action={
            <Button variant="secondary" onClick={() => setVersion((n) => n + 1)}>
              Try again
            </Button>
          }
        >
          {loadError(current.error)}
        </Alert>
      </>
    );
  }

  const { verification } = current;
  const program = verification.program_id ? (programs.get(verification.program_id) ?? 'Program of your faculty') : undefined;

  return (
    <>
      <h1>{verification.name ? `Registration of ${verification.name}` : 'Student registration'}</h1>
      {success && <Alert kind="success">{success}</Alert>}
      {failure && (
        <div ref={alertRef} tabIndex={-1}>
          <Alert>{failure}</Alert>
        </div>
      )}

      <dl className="details">
        <dt>Status</dt>
        <dd>
          <VerificationStatusBadge status={verification.status} />
        </dd>
        {verification.name && (
          <>
            <dt>Name</dt>
            <dd>{verification.name}</dd>
          </>
        )}
        {verification.email && (
          <>
            <dt>University email</dt>
            <dd>{verification.email}</dd>
          </>
        )}
        {verification.student_number && (
          <>
            <dt>Student ID</dt>
            <dd>{verification.student_number}</dd>
          </>
        )}
        {program && (
          <>
            <dt>Program</dt>
            <dd>{program}</dd>
          </>
        )}
        <dt>Submitted</dt>
        <dd>{formatDateTime(verification.submitted_at)}</dd>
        {verification.reviewed_at && (
          <>
            <dt>Decided</dt>
            <dd>{formatDateTime(verification.reviewed_at)}</dd>
          </>
        )}
        {verification.rejection_reason && (
          <>
            <dt>Rejection reason</dt>
            <dd className="preserve-lines">{verification.rejection_reason}</dd>
          </>
        )}
      </dl>
      {!verification.name && (
        <p className="muted">The student has moved to another faculty, so their current details are no longer shown here.</p>
      )}

      {verification.status === 'PENDING' && !success && (
        <section aria-labelledby="decision-heading">
          <h2 id="decision-heading">Decision</h2>
          <p>Check the name, student ID and program against the university’s records before you decide.</p>
          <DecisionPanel onApprove={() => decide(null)} onReject={(reason) => decide(reason)} />
        </section>
      )}
    </>
  );
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
    const problem = reasonProblem(reason);
    setReasonError(problem);
    if (problem) {
      signalFailure();
      return;
    }
    setBusy(true);
    try {
      await onReject(reason.trim());
    } catch {
      // VerificationReview already shows what went wrong; keep the reason so it can be sent again.
    } finally {
      setBusy(false);
    }
  }

  if (rejecting) {
    return (
      <form ref={formRef} noValidate className="form" onSubmit={(event) => void handleReject(event)}>
        <TextArea
          label="Reason for rejecting"
          hint="The student sees this reason when they log in, and can correct their details and submit again."
          name="rejection-reason"
          rows={4}
          autoFocus
          value={reason}
          onChange={(event) => {
            setReason(event.target.value);
            setReasonError(undefined);
          }}
          error={reasonError}
        />
        <div className="button-row">
          <Button type="submit" loading={busy}>
            Reject registration
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
        question="Approve this registration? The student can then find tutors, book sessions and apply to tutor."
        confirmLabel="Yes, approve"
        onConfirm={onApprove}
      />
      <Button variant="link" onClick={() => setRejecting(true)}>
        Reject
      </Button>
    </div>
  );
}
