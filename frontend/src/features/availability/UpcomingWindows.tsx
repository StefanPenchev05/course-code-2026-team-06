import { useEffect, useState } from 'react';
import { errorMessage } from '../../shared/api/errors';
import { Alert, Button, ConfirmAction, Spinner } from '../../shared/ui';
import { browserTimeZone, groupByDay, MODE_LABEL, timeRange } from '../../shared/utils/datetime';
import { availabilityApi } from './api';
import { availabilityError } from './errors';
import type { Window } from './types';

const DAY_MS = 24 * 60 * 60 * 1000;
const WEEKS = 4;

type Load = { key: string; status: 'ok'; windows: Window[]; hasMore: boolean } | { key: string; status: 'error'; message: string };

interface UpcomingWindowsProps {
  /** Changes whenever another part of the page changed availability, to reload this list. */
  reloadKey: number;
  /** Changes on live updates (#92): refetch without hiding this list's own confirmation. */
  liveKey?: number;
  /** Called after a window was removed, so the rest of the page can reload too. */
  onChanged: () => void;
  timeZone?: string;
}

/** The tutor's published windows for the next four weeks (owner view: booked time is included). */
export function UpcomingWindows({ reloadKey, liveKey = 0, onChanged, timeZone = browserTimeZone() }: UpcomingWindowsProps) {
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<Load | null>(null);
  const [removeErrors, setRemoveErrors] = useState<Record<string, string>>({});
  // A notice belongs to the change that caused it; any later change on the page hides it.
  const [notice, setNotice] = useState<{ text: string; reloadKey: number } | null>(null);
  const key = `${reloadKey}#${attempt}#${liveKey}`;

  useEffect(() => {
    const controller = new AbortController();
    const from = new Date();
    const to = new Date(from.getTime() + WEEKS * 7 * DAY_MS);
    availabilityApi.windows(from, to, 0, controller.signal).then(
      (page) => setLoad({ key, status: 'ok', windows: page.data, hasMore: page.pagination.has_more }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ key, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [key]);

  async function remove(window: Window) {
    setRemoveErrors((previous) => ({ ...previous, [window.id]: '' }));
    setNotice(null);
    try {
      await availabilityApi.removeWindow(window.id);
    } catch (error) {
      setRemoveErrors((previous) => ({ ...previous, [window.id]: availabilityError(error) }));
      throw error;
    }
    setNotice({
      text: window.is_recurring
        ? 'Removed. That date is now skipped for its weekly time; you can restore it under “Weekly times”.'
        : 'Removed.',
      // onChanged() below moves the page to the next reload key.
      reloadKey: reloadKey + 1,
    });
    onChanged();
  }

  // Keep the last list on screen while reloading after a change (no flashing spinner).
  const current = load;
  const refreshing = load !== null && load.key !== key;

  return (
    <section aria-labelledby="upcoming-heading" aria-busy={refreshing || undefined}>
      <h2 id="upcoming-heading">Upcoming free times</h2>
      <p className="muted">The next {WEEKS} weeks, shown in your time zone ({timeZone}). Times students booked are included.</p>
      {notice?.reloadKey === reloadKey && <Alert kind="success">{notice.text}</Alert>}
      {!current && <Spinner label="Loading your free times…" />}
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
      {current?.status === 'ok' && current.windows.length === 0 && (
        <p>You have no free times in the next {WEEKS} weeks. Add one below so students can book you.</p>
      )}
      {current?.status === 'ok' && current.windows.length > 0 && (
        <div className="day-list">
          {groupByDay(current.windows, timeZone).map((day) => (
            <div key={day.key} className="day">
              <h3 className="day__title">{day.label}</h3>
              <ul className="window-list">
                {day.items.map((window) => {
                  const when = `${day.label}, ${timeRange(window, timeZone)}`;
                  return (
                    <li key={window.id} className="window">
                      <span>
                        <span className="slot__time">{timeRange(window, timeZone)}</span>{' '}
                        <span className="muted">
                          {MODE_LABEL[window.allowed_mode] ?? window.allowed_mode} · {window.is_recurring ? 'Weekly' : 'One-off'}
                        </span>
                      </span>
                      <ConfirmAction
                        label="Remove"
                        ariaLabel={`Remove ${when}`}
                        question={window.is_recurring ? 'Skip this date?' : 'Remove this time?'}
                        confirmLabel={window.is_recurring ? 'Yes, skip it' : 'Yes, remove'}
                        onConfirm={() => remove(window)}
                      />
                      {removeErrors[window.id] && <Alert>{removeErrors[window.id]}</Alert>}
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </div>
      )}
      {current?.status === 'ok' && current.hasMore && (
        <p className="muted">Only the first 100 free times are shown.</p>
      )}
    </section>
  );
}
