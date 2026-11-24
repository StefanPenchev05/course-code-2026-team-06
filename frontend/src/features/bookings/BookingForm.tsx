import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, SelectField, TextArea } from '../../shared/ui';
import { browserTimeZone, MODE_LABEL, type SessionMode } from '../../shared/utils/datetime';
import { sessionsApi } from './api';
import { bookingError, byteLength, DESCRIPTION_MAX_BYTES, PURPOSES, startOptions } from './rules';
import type { BookedMode, Purpose, Session } from './types';

/** What the form needs to know about the tutor's offerings and the chosen free time. */
export interface BookableOffering {
  id: string;
  subject_id: string;
  subject_name: string;
}

export interface BookableWindow {
  /** The source availability slot ID. */
  id: string;
  starts_at: string;
  ends_at: string;
  allowed_mode: SessionMode;
}

interface BookingFormProps {
  tutorName: string;
  offerings: BookableOffering[];
  window: BookableWindow;
  /** Pre-select this subject (e.g. the one the student searched for). */
  subjectId?: string | null;
  onCancel: () => void;
  onBooked: (session: Session) => void;
  timeZone?: string;
  /** Injected for tests. */
  now?: Date;
}

/** Step 2 of booking: choose subject, purpose, length, start, mode and describe the need; then send. */
export function BookingForm({ tutorName, offerings, window, subjectId, onCancel, onBooked, timeZone = browserTimeZone(), now }: BookingFormProps) {
  const [offeringId, setOfferingId] = useState(
    () => offerings.find((offering) => offering.subject_id === subjectId)?.id ?? offerings[0]?.id ?? '',
  );
  const [purpose, setPurpose] = useState<Purpose>('EXAM_PREPARATION');
  const durations = PURPOSES.find((p) => p.value === purpose)!.minutes;
  const [minutes, setMinutes] = useState(60);
  const duration = durations.includes(minutes) ? minutes : durations[0];
  const [startedAt] = useState(() => now ?? new Date());
  const starts = startOptions(window, duration, startedAt);
  const [start, setStart] = useState('');
  const startValue = starts.some((option) => option.toISOString() === start) ? start : (starts[0]?.toISOString() ?? '');
  const fixedMode: BookedMode | null = window.allowed_mode === 'BOTH' ? null : window.allowed_mode;
  const [mode, setMode] = useState<BookedMode>('ONLINE');
  const [description, setDescription] = useState('');
  const [descriptionError, setDescriptionError] = useState<string | undefined>();
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();
  const headingRef = useRef<HTMLHeadingElement>(null);

  // Opening the form moves focus to it, so keyboard and screen-reader users land in the next step.
  useEffect(() => headingRef.current?.focus(), [window.id, window.starts_at]);

  const time = new Intl.DateTimeFormat(undefined, { timeZone, hour: '2-digit', minute: '2-digit' });
  const day = new Intl.DateTimeFormat(undefined, { timeZone, weekday: 'long', day: 'numeric', month: 'long' });

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;
    const text = description.trim();
    const problem = !text
      ? 'Tell the tutor briefly what you need help with.'
      : byteLength(text) > DESCRIPTION_MAX_BYTES
        ? 'This is too long. Keep it short; you can explain more in the session.'
        : undefined;
    setDescriptionError(problem);
    setFormError(null);
    if (problem || !startValue || !offeringId) {
      signalFailure();
      return;
    }
    setSubmitting(true);
    try {
      const session = await sessionsApi.request({
        offering_id: offeringId,
        availability_slot_id: window.id,
        starts_at: startValue,
        duration_minutes: duration,
        mode: fixedMode ?? mode,
        purpose,
        description: text,
      });
      onBooked(session);
    } catch (error) {
      setFormError(bookingError(error));
      setSubmitting(false);
      signalFailure();
    }
  }

  return (
    <section aria-labelledby="booking-heading" className="booking">
      <h2 id="booking-heading" ref={headingRef} tabIndex={-1}>
        Book {tutorName} on {day.format(new Date(window.starts_at))}
      </h2>
      <p className="muted">
        Free {time.format(new Date(window.starts_at))}–{time.format(new Date(window.ends_at))} ({timeZone}). The tutor confirms or
        declines your request.
      </p>
      <form ref={formRef} noValidate onSubmit={(event) => void handleSubmit(event)} className="form">
        {formError && (
          <div ref={alertRef} tabIndex={-1}>
            <Alert>{formError}</Alert>
          </div>
        )}
        {offerings.length > 1 && (
          <SelectField label="Subject" name="offering" value={offeringId} onChange={(e) => setOfferingId(e.target.value)}>
            {offerings.map((offering) => (
              <option key={offering.id} value={offering.id}>
                {offering.subject_name}
              </option>
            ))}
          </SelectField>
        )}
        {offerings.length === 1 && (
          <p>
            <strong>Subject:</strong> {offerings[0].subject_name}
          </p>
        )}
        <div className="filters__row">
          <SelectField label="Purpose" name="purpose" value={purpose} onChange={(e) => setPurpose(e.target.value as Purpose)}>
            {PURPOSES.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </SelectField>
          <SelectField
            label="Length"
            name="duration"
            value={String(duration)}
            onChange={(e) => setMinutes(Number(e.target.value))}
            hint={durations.length === 1 ? 'An introduction is always 30 minutes.' : undefined}
          >
            {durations.map((value) => (
              <option key={value} value={value}>
                {value} minutes
              </option>
            ))}
          </SelectField>
          {starts.length > 0 && (
            <SelectField label="Start" name="start" value={startValue} onChange={(e) => setStart(e.target.value)}>
              {starts.map((option) => (
                <option key={option.toISOString()} value={option.toISOString()}>
                  {time.format(option)}–{time.format(new Date(option.getTime() + duration * 60 * 1000))}
                </option>
              ))}
            </SelectField>
          )}
          {fixedMode ? (
            <p>
              <strong>Where:</strong> {MODE_LABEL[fixedMode]}
            </p>
          ) : (
            <SelectField label="Where" name="mode" value={mode} onChange={(e) => setMode(e.target.value as BookedMode)}>
              <option value="ONLINE">{MODE_LABEL.ONLINE}</option>
              <option value="ONSITE">{MODE_LABEL.ONSITE}</option>
            </SelectField>
          )}
        </div>
        {starts.length === 0 && (
          <Alert kind="info">
            A {duration}-minute session doesn’t fit in this free time (sessions need at least 3 hours’ notice). Choose a shorter length or
            another time.
          </Alert>
        )}
        <TextArea
          label="What do you need help with?"
          name="description"
          value={description}
          onChange={(e) => {
            setDescription(e.target.value);
            setDescriptionError(undefined);
          }}
          error={descriptionError}
          hint="For example the topic, exercise or exam. Only you and the tutor see this."
        />
        <div className="button-row">
          <Button type="submit" loading={submitting} disabled={starts.length === 0}>
            Send request
          </Button>
          <Button variant="secondary" onClick={onCancel} disabled={submitting}>
            Choose another time
          </Button>
        </div>
      </form>
    </section>
  );
}
