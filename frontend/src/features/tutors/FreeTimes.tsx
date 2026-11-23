import { useEffect, useState } from 'react';
import { errorMessage } from '../../shared/api/errors';
import { Alert, Button, Spinner } from '../../shared/ui';
import { tutorsApi } from './api';
import { browserTimeZone, groupByDay, MODE_LABEL, timeRange } from '../../shared/utils/datetime';
import type { FreeSlot } from './types';

const DAY_MS = 24 * 60 * 60 * 1000;
const WINDOW_DAYS = 14;
const MAX_PAGES = 5; // up to 500 intervals per window; more than enough for two weeks

type Load = { key: string; status: 'ok'; slots: FreeSlot[] } | { key: string; status: 'error'; message: string };

async function loadWindow(tutorId: string, from: Date, to: Date, signal: AbortSignal): Promise<FreeSlot[]> {
  const slots: FreeSlot[] = [];
  for (let page = 0; page < MAX_PAGES; page++) {
    const result = await tutorsApi.availability(tutorId, from, to, slots.length, signal);
    slots.push(...result.data);
    if (!result.pagination.has_more) break;
  }
  return slots;
}

interface FreeTimesProps {
  tutorId: string;
  /** Injected for tests; defaults to now and the browser's time zone. */
  now?: Date;
  timeZone?: string;
}

/**
 * The tutor's free time, two weeks at a time. This is a snapshot: the booking
 * step (#32) re-checks availability on the server.
 */
export function FreeTimes({ tutorId, now, timeZone = browserTimeZone() }: FreeTimesProps) {
  const [start] = useState(() => now ?? new Date());
  const [windowIndex, setWindowIndex] = useState(0);
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<Load | null>(null);

  const from = new Date(start.getTime() + windowIndex * WINDOW_DAYS * DAY_MS);
  const to = new Date(from.getTime() + WINDOW_DAYS * DAY_MS);
  const key = `${tutorId}#${from.toISOString()}#${attempt}`;

  useEffect(() => {
    const controller = new AbortController();
    const windowStart = new Date(start.getTime() + windowIndex * WINDOW_DAYS * DAY_MS);
    const windowEnd = new Date(windowStart.getTime() + WINDOW_DAYS * DAY_MS);
    loadWindow(tutorId, windowStart, windowEnd, controller.signal).then(
      (slots) => setLoad({ key, status: 'ok', slots }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ key, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [tutorId, start, windowIndex, key]);

  const current = load?.key === key ? load : null;
  const range = new Intl.DateTimeFormat(undefined, { timeZone, day: 'numeric', month: 'long' });

  return (
    <section aria-labelledby="free-times-heading">
      <h2 id="free-times-heading">Free times</h2>
      <p className="muted">
        {range.format(from)} – {range.format(new Date(to.getTime() - 1))}, shown in your time zone ({timeZone}).
      </p>

      {!current && <Spinner label="Loading free times…" />}
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
      {current?.status === 'ok' && current.slots.length === 0 && (
        <p>No free time in these two weeks.</p>
      )}
      {current?.status === 'ok' && current.slots.length > 0 && (
        <div className="day-list">
          {groupByDay(current.slots, timeZone).map((day) => (
            <div key={day.key} className="day">
              <h3 className="day__title">{day.label}</h3>
              <ul className="slot-list">
                {day.items.map((slot) => (
                  <li key={`${slot.id}-${slot.starts_at}`} className="slot">
                    <span className="slot__time">{timeRange(slot, timeZone)}</span>{' '}
                    <span className="muted">{MODE_LABEL[slot.allowed_mode] ?? slot.allowed_mode}</span>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}

      <div className="button-row">
        {windowIndex > 0 && (
          <Button variant="secondary" onClick={() => setWindowIndex((n) => n - 1)}>
            Previous two weeks
          </Button>
        )}
        <Button variant="secondary" onClick={() => setWindowIndex((n) => n + 1)}>
          Next two weeks
        </Button>
      </div>
    </section>
  );
}
