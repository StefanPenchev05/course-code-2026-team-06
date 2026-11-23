import { useState, type FormEvent } from 'react';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, TextArea } from '../../shared/ui';
import { browserTimeZone, MODE_LABEL } from '../../shared/utils/datetime';
import { sessionsApi } from './api';
import { bookingError, byteLength, isLateCancellation, isWebLink, PURPOSE_LABEL, REASON_MAX_BYTES, STATUS_LABEL } from './rules';
import type { Session, SessionStatus } from './types';

const CANCELLABLE: SessionStatus[] = ['REQUESTED', 'ACCEPTED'];

interface SessionCardProps {
  session: Session;
  onChanged: (session: Session) => void;
  timeZone?: string;
}

/** One session as the viewer sees it: what, when, status, their role, and what they can do. */
export function SessionCard({ session, onChanged, timeZone = browserTimeZone() }: SessionCardProps) {
  const [cancelling, setCancelling] = useState(false);
  const when = new Intl.DateTimeFormat(undefined, { timeZone, weekday: 'long', day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' });
  const until = new Intl.DateTimeFormat(undefined, { timeZone, hour: '2-digit', minute: '2-digit' });
  const role = session.viewer_role === 'TUTOR' ? 'You are the tutor' : 'You are the student';
  const byMe = session.cancelled_by_user_id && session.cancelled_by_user_id === (session.viewer_role === 'TUTOR' ? session.tutor_id : session.student_id);

  return (
    <li className="card session">
      <div className="session__head">
        <h3 className="card__title">{session.subject_name}</h3>
        <span className={`badge badge--${session.status.toLowerCase()}`}>{STATUS_LABEL[session.status] ?? session.status}</span>
      </div>
      <p className="card__meta">
        {when.format(new Date(session.starts_at))}–{until.format(new Date(session.ends_at))} · {session.duration_minutes} min ·{' '}
        {MODE_LABEL[session.mode]} · {PURPOSE_LABEL[session.purpose] ?? session.purpose}
      </p>
      <p className="card__meta">{role}</p>
      <p>{session.description}</p>

      {session.status === 'REQUESTED' && (
        <p className="muted">The tutor has until {when.format(new Date(session.expires_at))} to answer; otherwise the request expires.</p>
      )}
      {session.status === 'ACCEPTED' && session.meeting_link && (
        <p>
          Meeting link:{' '}
          {isWebLink(session.meeting_link) ? (
            <a href={session.meeting_link} target="_blank" rel="noreferrer noopener">
              {session.meeting_link}
            </a>
          ) : (
            session.meeting_link
          )}
        </p>
      )}
      {session.status === 'ACCEPTED' && session.meeting_room && <p>Room: {session.meeting_room}</p>}
      {session.status === 'DECLINED' && session.decline_reason && <p>Reason: {session.decline_reason}</p>}
      {session.status === 'CANCELLED' && (
        <p>
          {byMe ? 'You cancelled' : 'Cancelled'}
          {session.is_late_cancellation ? ' less than 24 hours before the start' : ''}
          {session.cancellation_reason ? `. Reason: ${session.cancellation_reason}` : '.'}
        </p>
      )}

      {CANCELLABLE.includes(session.status) &&
        (cancelling ? (
          <CancelForm
            session={session}
            onCancel={() => setCancelling(false)}
            onCancelled={(updated) => {
              setCancelling(false);
              onChanged(updated);
            }}
          />
        ) : (
          <Button variant="link" onClick={() => setCancelling(true)} aria-label={`Cancel ${session.subject_name} on ${when.format(new Date(session.starts_at))}`}>
            Cancel session
          </Button>
        ))}
    </li>
  );
}

function CancelForm({ session, onCancel, onCancelled }: { session: Session; onCancel: () => void; onCancelled: (session: Session) => void }) {
  const [reason, setReason] = useState('');
  const [reasonError, setReasonError] = useState<string | undefined>();
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();
  const late = isLateCancellation(session.starts_at);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const text = reason.trim();
    const problem = !text ? 'Give a reason; the other person sees it.' : byteLength(text) > REASON_MAX_BYTES ? 'This reason is too long.' : undefined;
    setReasonError(problem);
    setFormError(null);
    if (problem) {
      signalFailure();
      return;
    }
    setSubmitting(true);
    try {
      onCancelled(await sessionsApi.cancel(session.id, text));
    } catch (error) {
      setFormError(bookingError(error));
      setSubmitting(false);
      signalFailure();
    }
  }

  return (
    <form ref={formRef} noValidate onSubmit={(event) => void submit(event)} className="form cancel-form" aria-label="Cancel session">
      {formError && (
        <div ref={alertRef} tabIndex={-1}>
          <Alert>{formError}</Alert>
        </div>
      )}
      {late && (
        <Alert kind="warning" title="This is a late cancellation">
          The session starts in less than 24 hours. Late cancellations are recorded, and repeated ones are reported to your faculty
          coordinator.
        </Alert>
      )}
      <TextArea
        label="Reason for cancelling"
        name="reason"
        value={reason}
        onChange={(event) => {
          setReason(event.target.value);
          setReasonError(undefined);
        }}
        error={reasonError}
        hint="The other person sees this reason."
      />
      <div className="button-row">
        <Button type="submit" variant="secondary" loading={submitting}>
          Cancel the session
        </Button>
        <Button variant="link" onClick={onCancel} disabled={submitting}>
          Keep it
        </Button>
      </div>
    </form>
  );
}
