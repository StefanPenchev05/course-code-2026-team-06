import { useState, type FormEvent } from 'react';
import { ApiError } from '../../shared/api/client';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, ConfirmAction, TextArea, TextField } from '../../shared/ui';
import { browserTimeZone, MODE_LABEL } from '../../shared/utils/datetime';
import { sessionsApi } from './api';
import {
  bookingError,
  byteLength,
  hasEnded,
  isWebLink,
  MEETING_ROOM_MAX_BYTES,
  PURPOSE_LABEL,
  REASON_MAX_BYTES,
  statusLabel,
  validMeetingLink,
} from './rules';
import type { Session } from './types';

interface TutorSessionCardProps {
  session: Session;
  /** Called with the updated session after accept, decline or completion. */
  onChanged: (session: Session) => void;
  /** Called when the server says the session changed meanwhile (409), so the list refetches it. */
  onStale?: () => void;
  timeZone?: string;
  /** Injected for tests. */
  now?: Date;
}

type Panel = 'none' | 'accept' | 'decline';

/**
 * A session from the tutor's side. Only the next valid step is offered:
 * REQUESTED → accept or decline; ACCEPTED → complete once it has ended.
 * Other states (declined, expired, cancelled, completed, no-show) have no actions.
 */
export function TutorSessionCard({ session, onChanged, onStale, timeZone = browserTimeZone(), now }: TutorSessionCardProps) {
  const [panel, setPanel] = useState<Panel>('none');
  const [completeError, setCompleteError] = useState<string | null>(null);
  const when = new Intl.DateTimeFormat(undefined, { timeZone, weekday: 'long', day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' });
  const until = new Intl.DateTimeFormat(undefined, { timeZone, hour: '2-digit', minute: '2-digit' });
  const label = `${session.subject_name} on ${when.format(new Date(session.starts_at))}`;

  async function complete() {
    setCompleteError(null);
    try {
      onChanged(await sessionsApi.complete(session.id));
    } catch (error) {
      setCompleteError(bookingError(error));
      if (error instanceof ApiError && error.status === 409) onStale?.();
      throw error;
    }
  }

  return (
    <li className="card session">
      <div className="session__head">
        <h3 className="card__title">{session.subject_name}</h3>
        <span className={`badge badge--${session.status.toLowerCase()}`}>{statusLabel(session)}</span>
      </div>
      <p className="card__meta">
        {when.format(new Date(session.starts_at))}–{until.format(new Date(session.ends_at))} · {session.duration_minutes} min ·{' '}
        {MODE_LABEL[session.mode]} · {PURPOSE_LABEL[session.purpose] ?? session.purpose}
      </p>
      <p>
        <span className="muted">The student wrote:</span> {session.description}
      </p>

      {session.status === 'REQUESTED' && (
        <>
          <p className="muted">Answer by {when.format(new Date(session.expires_at))}, otherwise the request expires.</p>
          {panel === 'none' && (
            <div className="button-row">
              <Button onClick={() => setPanel('accept')} aria-label={`Accept ${label}`}>
                Accept
              </Button>
              <Button variant="secondary" onClick={() => setPanel('decline')} aria-label={`Decline ${label}`}>
                Decline
              </Button>
            </div>
          )}
          {panel === 'accept' && <AcceptForm session={session} onCancel={() => setPanel('none')} onDone={onChanged} onStale={onStale} />}
          {panel === 'decline' && <DeclineForm session={session} onCancel={() => setPanel('none')} onDone={onChanged} onStale={onStale} />}
        </>
      )}

      {session.status === 'ACCEPTED' && (
        <>
          {session.meeting_link && (
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
          {session.meeting_room && <p>Room: {session.meeting_room}</p>}
          {hasEnded(session.ends_at, now) ? (
            <ConfirmAction
              label="Mark as completed"
              ariaLabel={`Mark as completed: ${label}`}
              question="Did this session take place?"
              confirmLabel="Yes, it took place"
              onConfirm={complete}
            />
          ) : (
            <p className="muted">You can mark it as completed after it ends ({until.format(new Date(session.ends_at))}).</p>
          )}
          {completeError && <Alert>{completeError}</Alert>}
        </>
      )}
    </li>
  );
}

function AcceptForm({
  session,
  onCancel,
  onDone,
  onStale,
}: {
  session: Session;
  onCancel: () => void;
  onDone: (session: Session) => void;
  onStale?: () => void;
}) {
  const online = session.mode === 'ONLINE';
  const [value, setValue] = useState('');
  const [fieldError, setFieldError] = useState<string | undefined>();
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const text = value.trim();
    const problem = online
      ? validMeetingLink(text)
        ? undefined
        : 'Enter a meeting link that starts with https:// (for example a Teams or Zoom link).'
      : !text
        ? 'Enter the room or place where you will meet.'
        : byteLength(text) > MEETING_ROOM_MAX_BYTES
          ? 'This is too long.'
          : undefined;
    setFieldError(problem);
    setFormError(null);
    if (problem) {
      signalFailure();
      return;
    }
    setSubmitting(true);
    try {
      onDone(await sessionsApi.accept(session.id, online ? { meeting_link: text } : { meeting_room: text }));
    } catch (error) {
      setFormError(bookingError(error));
      setSubmitting(false);
      signalFailure();
      if (error instanceof ApiError && error.status === 409) onStale?.();
    }
  }

  return (
    <form ref={formRef} noValidate onSubmit={(event) => void submit(event)} className="form cancel-form" aria-label="Accept session">
      {formError && (
        <div ref={alertRef} tabIndex={-1}>
          <Alert>{formError}</Alert>
        </div>
      )}
      <TextField
        label={online ? 'Meeting link' : 'Room or place'}
        name={online ? 'meeting_link' : 'meeting_room'}
        type={online ? 'url' : 'text'}
        value={value}
        onChange={(event) => {
          setValue(event.target.value);
          setFieldError(undefined);
        }}
        error={fieldError}
        hint={online ? 'The student gets this link to join the online session.' : 'Where you will meet, e.g. a room number.'}
      />
      <div className="button-row">
        <Button type="submit" loading={submitting}>
          Accept the session
        </Button>
        <Button variant="link" onClick={onCancel} disabled={submitting}>
          Back
        </Button>
      </div>
    </form>
  );
}

function DeclineForm({
  session,
  onCancel,
  onDone,
  onStale,
}: {
  session: Session;
  onCancel: () => void;
  onDone: (session: Session) => void;
  onStale?: () => void;
}) {
  const [reason, setReason] = useState('');
  const [reasonError, setReasonError] = useState<string | undefined>();
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const text = reason.trim();
    const problem = !text ? 'Give a reason; the student sees it.' : byteLength(text) > REASON_MAX_BYTES ? 'This reason is too long.' : undefined;
    setReasonError(problem);
    setFormError(null);
    if (problem) {
      signalFailure();
      return;
    }
    setSubmitting(true);
    try {
      onDone(await sessionsApi.decline(session.id, text));
    } catch (error) {
      setFormError(bookingError(error));
      setSubmitting(false);
      signalFailure();
      if (error instanceof ApiError && error.status === 409) onStale?.();
    }
  }

  return (
    <form ref={formRef} noValidate onSubmit={(event) => void submit(event)} className="form cancel-form" aria-label="Decline session">
      {formError && (
        <div ref={alertRef} tabIndex={-1}>
          <Alert>{formError}</Alert>
        </div>
      )}
      <TextArea
        label="Reason for declining"
        name="reason"
        value={reason}
        onChange={(event) => {
          setReason(event.target.value);
          setReasonError(undefined);
        }}
        error={reasonError}
        hint="The student sees this reason."
      />
      <div className="button-row">
        <Button type="submit" variant="secondary" loading={submitting}>
          Decline the session
        </Button>
        <Button variant="link" onClick={onCancel} disabled={submitting}>
          Back
        </Button>
      </div>
    </form>
  );
}
