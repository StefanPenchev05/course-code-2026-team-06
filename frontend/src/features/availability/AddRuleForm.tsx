import { useEffect, useState, type FormEvent } from 'react';
import { errorMessage } from '../../shared/api/errors';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, SelectField, Spinner, TextField } from '../../shared/ui';
import { browserTimeZone, MODE_LABEL, type SessionMode } from '../../shared/utils/datetime';
import { availabilityApi } from './api';
import { availabilityError } from './errors';
import { defaultSemester, WEEKDAYS } from './format';
import type { Semester } from './types';
import { isTimeZone, validateRule, type RuleForm } from './validation';

type Semesters = { status: 'ok'; items: Semester[] } | { status: 'error'; message: string };

/** Adds a weekly time for one semester (POST /me/availability/rules), e.g. every Monday 14:00–16:00. */
export function AddRuleForm({ onAdded }: { onAdded: () => void }) {
  const [semesters, setSemesters] = useState<Semesters | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [semesterId, setSemesterId] = useState('');
  const [form, setForm] = useState<RuleForm>({ weekday: '1', start: '', end: '', zone: browserTimeZone(), mode: 'BOTH' });
  const [errors, setErrors] = useState<Partial<Record<keyof RuleForm | 'semester', string>>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [added, setAdded] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  useEffect(() => {
    const controller = new AbortController();
    availabilityApi.semesters(controller.signal).then(
      (items) => setSemesters({ status: 'ok', items }),
      (error: unknown) => {
        if (!controller.signal.aborted) setSemesters({ status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [attempt]);

  const items = semesters?.status === 'ok' ? semesters.items : [];
  const chosenSemester = items.some((s) => s.id === semesterId) ? semesterId : defaultSemester(items);
  const range = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', year: 'numeric' });

  function update(field: keyof RuleForm, value: string) {
    setForm((previous) => ({ ...previous, [field]: value }));
    setErrors((previous) => (previous[field] ? { ...previous, [field]: undefined } : previous));
    setAdded(false);
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;
    const found: Partial<Record<keyof RuleForm | 'semester', string>> = { ...validateRule(form) };
    if (!chosenSemester) found.semester = 'Choose a semester.';
    if (!isTimeZone(form.zone)) found.zone = 'Enter a time zone such as Europe/Luxembourg.';
    setErrors(found);
    setFormError(null);
    setAdded(false);
    if (Object.values(found).some(Boolean)) {
      signalFailure();
      return;
    }
    setSubmitting(true);
    try {
      await availabilityApi.createRule({
        semester_id: chosenSemester,
        iso_weekday: Number(form.weekday),
        local_start_time: form.start,
        local_end_time: form.end,
        time_zone: form.zone.trim(),
        allowed_mode: form.mode,
      });
      setForm((previous) => ({ ...previous, start: '', end: '' }));
      setAdded(true);
      onAdded();
    } catch (error) {
      setFormError(availabilityError(error));
      signalFailure();
    } finally {
      setSubmitting(false);
    }
  }

  if (!semesters) return <Spinner label="Loading semesters…" />;
  if (semesters.status === 'error') {
    return (
      <Alert
        action={
          <Button variant="secondary" onClick={() => setAttempt((n) => n + 1)}>
            Try again
          </Button>
        }
      >
        We couldn’t load the semesters, so weekly times can’t be added right now. {semesters.message}
      </Alert>
    );
  }
  if (items.length === 0) {
    return <Alert kind="info">No semester is open for planning yet, so weekly times can’t be added. One-off times still work.</Alert>;
  }

  return (
    <form ref={formRef} noValidate onSubmit={(event) => void submit(event)} className="form" aria-labelledby="add-rule-heading">
      <h3 id="add-rule-heading">Add a weekly time</h3>
      {formError && (
        <div ref={alertRef} tabIndex={-1}>
          <Alert>{formError}</Alert>
        </div>
      )}
      {added && <Alert kind="success">Added. Its free times until the end of the semester are now bookable.</Alert>}
      <div className="filters__row">
        <SelectField label="Semester" name="semester" value={chosenSemester} onChange={(e) => setSemesterId(e.target.value)} error={errors.semester}>
          {items.map((semester) => (
            <option key={semester.id} value={semester.id}>
              {semester.name} ({range.format(new Date(semester.starts_at))} – {range.format(new Date(semester.ends_at))})
            </option>
          ))}
        </SelectField>
        <SelectField label="Day" name="weekday" value={form.weekday} onChange={(e) => update('weekday', e.target.value)}>
          {WEEKDAYS.map((day, index) => (
            <option key={day} value={String(index + 1)}>
              {day}
            </option>
          ))}
        </SelectField>
        <TextField label="From" type="time" name="start" value={form.start} onChange={(e) => update('start', e.target.value)} error={errors.start} />
        <TextField label="Until" type="time" name="end" value={form.end} onChange={(e) => update('end', e.target.value)} error={errors.end} />
        <TextField
          label="Time zone"
          name="zone"
          list="time-zones"
          value={form.zone}
          onChange={(e) => update('zone', e.target.value)}
          error={errors.zone}
          hint="The clock times are in this zone; they stay the same across daylight saving."
        />
        <SelectField label="Where" name="mode" value={form.mode} onChange={(e) => update('mode', e.target.value as SessionMode)}>
          {(Object.keys(MODE_LABEL) as SessionMode[]).map((mode) => (
            <option key={mode} value={mode}>
              {MODE_LABEL[mode]}
            </option>
          ))}
        </SelectField>
      </div>
      <Button type="submit" loading={submitting}>
        Add weekly time
      </Button>
    </form>
  );
}
