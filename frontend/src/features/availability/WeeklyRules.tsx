import { useEffect, useState, type FormEvent } from 'react';
import { errorMessage } from '../../shared/api/errors';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, ConfirmAction, SelectField, Spinner, TextField } from '../../shared/ui';
import { MODE_LABEL, type SessionMode } from '../../shared/utils/datetime';
import { AddRuleForm } from './AddRuleForm';
import { availabilityApi } from './api';
import { availabilityError } from './errors';
import { hhmm, localDateText, ruleText, WEEKDAYS } from './format';
import type { Rule, SkippedDate } from './types';
import { changedFields, validateRule, type RuleForm } from './validation';

type Load<T> = { key: string; status: 'ok'; items: T[] } | { key: string; status: 'error'; message: string };

interface WeeklyRulesProps {
  reloadKey: number;
  /** Changes on live updates (#92): refetch, keeping open panels and editors. */
  liveKey?: number;
  onChanged: () => void;
}

/**
 * The tutor's weekly times. Times are local clock times in each rule's own
 * time zone, so "14:00 Europe/Luxembourg" stays 14:00 across daylight saving.
 */
export function WeeklyRules({ reloadKey, liveKey = 0, onChanged }: WeeklyRulesProps) {
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<Load<Rule> | null>(null);
  const key = `${reloadKey}#${attempt}#${liveKey}`;

  useEffect(() => {
    const controller = new AbortController();
    availabilityApi.rules(controller.signal).then(
      (page) => setLoad({ key, status: 'ok', items: page.data }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ key, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [key]);

  // While reloading after a change, keep showing the last list: replacing it with a spinner would
  // unmount the rules and close any open "Skipped dates" panel or editor.
  const current = load;
  const refreshing = load !== null && load.key !== key;

  return (
    <section aria-labelledby="weekly-heading" aria-busy={refreshing || undefined}>
      <h2 id="weekly-heading">Weekly times</h2>
      <p className="muted">Repeat every week until the end of the semester. Changing one updates its future free times.</p>
      {!current && <Spinner label="Loading weekly times…" />}
      {current?.status === 'error' && (
        <Alert
          action={
            <Button variant="secondary" onClick={() => setAttempt((n) => n + 1)}>
              Try again
            </Button>
          }
        >
          {current.message}
        </Alert>
      )}
      {current?.status === 'ok' && current.items.length === 0 && <p>You have no weekly times.</p>}
      {current?.status === 'ok' && current.items.length > 0 && (
        <ul className="card-list">
          {current.items.map((rule) => (
            <RuleItem key={rule.id} rule={rule} onChanged={onChanged} version={`${reloadKey}#${liveKey}`} />
          ))}
        </ul>
      )}
      <AddRuleForm onAdded={onChanged} />
      {/* One shared list of time-zone suggestions for the add and edit forms (ids must be unique). */}
      <datalist id="time-zones">
        {zoneList().map((zone) => (
          <option key={zone} value={zone} />
        ))}
      </datalist>
    </section>
  );
}

function RuleItem({ rule, onChanged, version }: { rule: Rule; onChanged: () => void; version: string }) {
  const [editing, setEditing] = useState(false);
  const [showSkipped, setShowSkipped] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const text = ruleText(rule);

  async function remove() {
    setError(null);
    try {
      await availabilityApi.deleteRule(rule.id);
    } catch (failure) {
      setError(availabilityError(failure));
      throw failure;
    }
    onChanged();
  }

  return (
    <li className="card">
      {editing ? (
        <RuleEditor
          rule={rule}
          onCancel={() => setEditing(false)}
          onSaved={() => {
            setEditing(false);
            onChanged();
          }}
        />
      ) : (
        <>
          <p className="card__title">{text}</p>
          <div className="button-row">
            <Button variant="link" onClick={() => setEditing(true)} aria-label={`Edit ${text}`}>
              Edit
            </Button>
            <Button variant="link" onClick={() => setShowSkipped((open) => !open)} aria-expanded={showSkipped} aria-label={`Skipped dates of ${text}`}>
              Skipped dates
            </Button>
            <ConfirmAction
              label="Delete"
              ariaLabel={`Delete ${text}`}
              question="Delete this weekly time and its future free times?"
              confirmLabel="Yes, delete"
              onConfirm={remove}
            />
          </div>
          {error && <Alert>{error}</Alert>}
          {showSkipped && <SkippedDates rule={rule} onChanged={onChanged} version={version} />}
        </>
      )}
    </li>
  );
}

function zoneList(): string[] {
  try {
    return Intl.supportedValuesOf('timeZone');
  } catch {
    return [];
  }
}

function RuleEditor({ rule, onCancel, onSaved }: { rule: Rule; onCancel: () => void; onSaved: () => void }) {
  const [form, setForm] = useState<RuleForm>({
    weekday: String(rule.iso_weekday),
    start: hhmm(rule.local_start_time),
    end: hhmm(rule.local_end_time),
    zone: rule.time_zone,
    mode: rule.allowed_mode,
  });
  const [errors, setErrors] = useState<Partial<Record<keyof RuleForm, string>>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  function update(field: keyof RuleForm, value: string) {
    setForm((previous) => ({ ...previous, [field]: value }));
    setErrors((previous) => (previous[field] ? { ...previous, [field]: undefined } : previous));
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const found = validateRule(form);
    setErrors(found);
    setFormError(null);
    if (Object.values(found).some(Boolean)) {
      signalFailure();
      return;
    }
    const patch = changedFields(rule, form);
    if (Object.keys(patch).length === 0) {
      onCancel();
      return;
    }
    setSaving(true);
    try {
      await availabilityApi.updateRule(rule.id, patch);
      onSaved();
    } catch (failure) {
      setFormError(availabilityError(failure));
      setSaving(false);
      signalFailure();
    }
  }

  return (
    <form ref={formRef} noValidate onSubmit={(event) => void save(event)} className="form" aria-label="Edit weekly time">
      {formError && (
        <div ref={alertRef} tabIndex={-1}>
          <Alert>{formError}</Alert>
        </div>
      )}
      <div className="filters__row">
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
          hint="The clock times are in this zone, e.g. Europe/Luxembourg."
        />
        <SelectField label="Where" name="mode" value={form.mode} onChange={(e) => update('mode', e.target.value as SessionMode)}>
          {(Object.keys(MODE_LABEL) as SessionMode[]).map((mode) => (
            <option key={mode} value={mode}>
              {MODE_LABEL[mode]}
            </option>
          ))}
        </SelectField>
      </div>
      <div className="button-row">
        <Button type="submit" loading={saving}>
          Save changes
        </Button>
        <Button variant="secondary" onClick={onCancel} disabled={saving}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

function SkippedDates({ rule, onChanged, version }: { rule: Rule; onChanged: () => void; version: string }) {
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<Load<SkippedDate> | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [restoring, setRestoring] = useState<string | null>(null);
  // `version` changes when the page reloads (own change or live update): refetch the skipped dates too.
  const key = `${rule.id}#${attempt}#${version}`;

  useEffect(() => {
    const controller = new AbortController();
    availabilityApi.skippedDates(rule.id, controller.signal).then(
      (page) => setLoad({ key, status: 'ok', items: page.data }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ key, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [rule.id, key]);

  async function restore(date: string) {
    setRestoring(date);
    setErrors((previous) => ({ ...previous, [date]: '' }));
    try {
      await availabilityApi.restoreDate(rule.id, date);
      setAttempt((n) => n + 1);
      onChanged();
    } catch (failure) {
      setErrors((previous) => ({ ...previous, [date]: availabilityError(failure) }));
    } finally {
      setRestoring(null);
    }
  }

  // Same rule: keep the shown dates while refetching (no spinner flash on live updates).
  const current = load?.key.startsWith(`${rule.id}#`) ? load : null;
  if (!current) return <Spinner label="Loading skipped dates…" />;
  if (current.status === 'error') return <Alert>{current.message}</Alert>;
  if (current.items.length === 0) return <p className="muted">No skipped dates.</p>;

  return (
    <ul className="window-list" aria-label="Skipped dates">
      {current.items.map((skipped) => (
        <li key={skipped.local_date} className="window">
          <span>{localDateText(skipped.local_date)}</span>
          <Button
            variant="link"
            onClick={() => void restore(skipped.local_date)}
            loading={restoring === skipped.local_date}
            aria-label={`Restore ${localDateText(skipped.local_date)}`}
          >
            Restore
          </Button>
          {errors[skipped.local_date] && <Alert>{errors[skipped.local_date]}</Alert>}
        </li>
      ))}
    </ul>
  );
}
