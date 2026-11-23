import { useState, type FormEvent } from 'react';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, SelectField, TextField } from '../../shared/ui';
import { browserTimeZone, MODE_LABEL, type SessionMode } from '../../shared/utils/datetime';
import { availabilityApi } from './api';
import { availabilityError } from './errors';
import { localInstant } from './format';
import { validateOneOff, type OneOffErrors, type OneOffValues } from './validation';

type FormValues = OneOffValues;
type Errors = OneOffErrors;

const EMPTY: FormValues = { date: '', start: '', end: '', mode: 'BOTH' };

/** Publishes a single free window (POST /me/availability/slots). */
export function OneOffForm({ onAdded, timeZone = browserTimeZone() }: { onAdded: () => void; timeZone?: string }) {
  const [values, setValues] = useState<FormValues>(EMPTY);
  const [errors, setErrors] = useState<Errors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [added, setAdded] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  function update(field: keyof FormValues, value: string) {
    setValues((previous) => ({ ...previous, [field]: value }));
    setErrors((previous) => (previous[field] ? { ...previous, [field]: undefined } : previous));
    setAdded(false);
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;
    const found = validateOneOff(values);
    setErrors(found);
    setFormError(null);
    setAdded(false);
    if (Object.values(found).some(Boolean)) {
      signalFailure();
      return;
    }
    setSubmitting(true);
    try {
      await availabilityApi.addWindow({
        starts_at: localInstant(values.date, values.start)!.toISOString(),
        ends_at: localInstant(values.date, values.end)!.toISOString(),
        allowed_mode: values.mode,
      });
      setValues(EMPTY);
      setAdded(true);
      onAdded();
    } catch (error) {
      setFormError(availabilityError(error));
      signalFailure();
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <section aria-labelledby="one-off-heading">
      <h2 id="one-off-heading">Add a one-off free time</h2>
      <p className="muted">For a single date. Times are in your time zone ({timeZone}) and must fall within a semester.</p>
      <form ref={formRef} noValidate onSubmit={(event) => void handleSubmit(event)} className="form">
        {formError && (
          <div ref={alertRef} tabIndex={-1}>
            <Alert>{formError}</Alert>
          </div>
        )}
        {added && <Alert kind="success">Added. Students can now book this time.</Alert>}
        <div className="filters__row">
          <TextField label="Date" type="date" name="date" value={values.date} onChange={(e) => update('date', e.target.value)} error={errors.date} />
          <TextField label="From" type="time" name="start" value={values.start} onChange={(e) => update('start', e.target.value)} error={errors.start} />
          <TextField label="Until" type="time" name="end" value={values.end} onChange={(e) => update('end', e.target.value)} error={errors.end} />
          <SelectField label="Where" name="mode" value={values.mode} onChange={(e) => update('mode', e.target.value)}>
            {(Object.keys(MODE_LABEL) as SessionMode[]).map((mode) => (
              <option key={mode} value={mode}>
                {MODE_LABEL[mode]}
              </option>
            ))}
          </SelectField>
        </div>
        <Button type="submit" loading={submitting}>
          Add free time
        </Button>
      </form>
    </section>
  );
}
