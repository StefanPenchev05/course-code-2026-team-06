import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, mockApi, type MockCall } from '../../test/mockApi';
import { coordinator, page, renderApp, TUTOR_ID, tutorSummary } from './testUtils';

// Component tests with a mocked API (test double, not integration evidence; see src/test/mockApi.ts).
// One tutor's monitoring page with policy cancellation (#35, FR16/FR19, UC12/UC04).

const HOUR = 3_600_000;
const TUTOR = `/api/v1/coordinator/tutors/${TUTOR_ID}`;
const SCHEDULE = `${TUTOR}/schedule`;
const FEEDBACK = `${TUTOR}/feedback`;
const CANCEL = '/api/v1/coordinator/sessions/ses-acc/cancel';
const start = Date.now() + 26 * HOUR;
const at = (offsetHours: number) => new Date(start + offsetHours * HOUR).toISOString();

const detail = {
  ...tutorSummary(),
  offerings: [
    { id: 'off-1', subject_id: 'sub-algo', subject_name: 'Algorithms', description: 'Exam preparation.', active: true },
    { id: 'off-2', subject_id: 'sub-db', subject_name: 'Databases', description: '', active: false },
  ],
};

function session(overrides: Record<string, unknown> = {}) {
  return { id: 'ses-acc', kind: 'SESSION', starts_at: at(0), ends_at: at(1), mode: 'ONLINE', status: 'ACCEPTED', subject_id: 'sub-algo', subject_name: 'Algorithms', semester_id: 'sem-1', ...overrides };
}

const week = [
  { id: 'slot-1', kind: 'AVAILABILITY', starts_at: at(-2), ends_at: at(3), mode: 'BOTH', rule_id: 'rule-1', time_zone: 'Europe/Luxembourg', semester_id: 'sem-1' },
  session(),
  session({ id: 'ses-done', starts_at: at(2), ends_at: at(3), status: 'COMPLETED', subject_id: 'sub-db', subject_name: 'Databases' }),
];

const feedback = {
  average_rating: 4.67,
  rating_count: 3,
  ...page([
    { session_id: 'ses-old', session_status: 'COMPLETED', subject_id: 'sub-algo', subject_name: 'Algorithms', starts_at: at(-200), rating: 5, comment: 'Clear and patient.', created_at: at(-190) },
  ]),
};

function mockTutor(routes: Record<string, (call: MockCall) => Response> = {}) {
  return mockApi({
    'GET /api/v1/auth/me': () => json(200, coordinator),
    [`GET ${TUTOR}`]: () => json(200, { data: detail, offerings_pagination: { limit: 100, offset: 0, has_more: false } }),
    [`GET ${SCHEDULE}`]: () => json(200, page(week, false, 100)),
    [`GET ${FEEDBACK}`]: () => json(200, feedback),
    ...routes,
  });
}

const region = (name: string) => screen.getByRole('region', { name });
const scheduleQueries = (calls: MockCall[]) =>
  calls.filter((c) => c.url.startsWith(`${SCHEDULE}?`)).map((c) => new URL(c.url, 'http://x').searchParams);
const scheduleEntry = async (subject: string) => (await within(await screen.findByRole('region', { name: 'Schedule' })).findByText(subject)).closest('li')!;

afterEach(() => vi.unstubAllGlobals());

describe('One tutor', () => {
  it('shows totals, subjects, the week’s schedule and private feedback', async () => {
    const { calls } = mockTutor();
    renderApp(`/coordinator/tutors/${TUTOR_ID}`);

    expect(await screen.findByRole('heading', { level: 1, name: 'Tom Tutor' })).toBeInTheDocument();
    expect(screen.getByText(/Bachelor in Computer Science · Tutor since/)).toBeInTheDocument();
    expect(screen.getByText('Hours taught').nextElementSibling).toHaveTextContent('6');
    expect(screen.getByText('Average rating').nextElementSibling).toHaveTextContent('4.7 / 5');

    const subjects = within(region('Subjects')).getAllByRole('listitem');
    expect(subjects).toHaveLength(2);
    expect(subjects[0]).toHaveTextContent('Exam preparation.');
    expect(within(subjects[1]).getByText('Inactive')).toBeInTheDocument();

    const confirmed = await scheduleEntry('Algorithms');
    expect(within(confirmed).getByText('Confirmed')).toBeInTheDocument();
    expect(within(confirmed).getByRole('button', { name: /^Cancel session: Algorithms/ })).toBeInTheDocument();
    const completed = await scheduleEntry('Databases');
    expect(within(completed).getByText('Completed')).toBeInTheDocument();
    expect(within(completed).queryByRole('button')).not.toBeInTheDocument();
    expect(within(region('Schedule')).getByText(/Free time · Online or on site · weekly/)).toBeInTheDocument();

    const comments = within(region('Feedback from students'));
    expect(await comments.findByText('Clear and patient.')).toBeInTheDocument();
    expect(comments.getByText(/5 out of 5/)).toBeInTheDocument();

    // One week, Monday 00:00 local time, and only the query keys the API accepts.
    const [query] = scheduleQueries(calls);
    const from = new Date(query.get('from')!);
    const to = new Date(query.get('to')!);
    expect([from.getDay(), from.getHours(), from.getMinutes()]).toEqual([1, 0, 0]);
    expect(to.getDate()).toBe(new Date(from.getFullYear(), from.getMonth(), from.getDate() + 7).getDate());
    expect(from.getTime()).toBeLessThanOrEqual(Date.now());
    expect([...query.keys()]).toEqual(['from', 'to', 'limit', 'offset']);
    expect(calls.find((c) => c.url.startsWith(`${TUTOR}?`))?.url).toBe(`${TUTOR}?limit=100&offset=0`);
  });

  it('moves between weeks', async () => {
    const { calls } = mockTutor();
    renderApp(`/coordinator/tutors/${TUTOR_ID}`);
    const user = userEvent.setup();

    await scheduleEntry('Algorithms');
    expect(screen.queryByRole('button', { name: 'This week' })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Next week' }));
    await waitFor(() => expect(scheduleQueries(calls)).toHaveLength(2));
    const [thisWeek, nextWeek] = scheduleQueries(calls);
    expect(nextWeek.get('from')).toBe(thisWeek.get('to'));

    await user.click(await screen.findByRole('button', { name: 'This week' }));
    await waitFor(() => expect(scheduleQueries(calls)).toHaveLength(3));
    expect(scheduleQueries(calls)[2].get('from')).toBe(thisWeek.get('from'));

    await user.click(screen.getByRole('button', { name: 'Previous week' }));
    await waitFor(() => expect(scheduleQueries(calls)).toHaveLength(4));
    expect(scheduleQueries(calls)[3].get('to')).toBe(thisWeek.get('from'));
  });

  it('cancels a confirmed session for policy reasons, with a required reason', async () => {
    const { calls } = mockTutor({
      [`POST ${CANCEL}`]: ({ body }) =>
        json(200, {
          data: {
            id: 'ses-acc',
            status: 'CANCELLED',
            starts_at: at(0),
            ends_at: at(1),
            cancelled_at: new Date().toISOString(),
            cancelled_by_user_id: coordinator.id,
            cancellation_reason: (body as { reason: string }).reason,
            cancellation_origin: 'COORDINATOR_POLICY',
            is_late_cancellation: false,
          },
        }),
    });
    renderApp(`/coordinator/tutors/${TUTOR_ID}`);
    const user = userEvent.setup();

    const entry = await scheduleEntry('Algorithms');
    await user.click(within(entry).getByRole('button', { name: /^Cancel session: Algorithms/ }));
    expect(within(entry).getByText('Cancel this session for policy reasons?')).toBeInTheDocument();

    await user.click(within(entry).getByRole('button', { name: 'Cancel the session' }));
    expect(within(entry).getByText('Give a reason; both participants can read it.')).toBeInTheDocument();
    expect(within(entry).getByLabelText('Reason for cancelling')).toHaveFocus();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);

    await user.type(within(entry).getByLabelText('Reason for cancelling'), '  Not in line with faculty policy.  ');
    await user.click(within(entry).getByRole('button', { name: 'Cancel the session' }));

    expect(await screen.findByText('The session was cancelled. Both participants are notified and can read your reason.')).toBeInTheDocument();
    expect(within(entry).getByText('Cancelled')).toBeInTheDocument();
    expect(within(entry).queryByRole('button')).not.toBeInTheDocument();
    expect(calls.filter((c) => c.method === 'POST')).toEqual([{ method: 'POST', url: CANCEL, body: { reason: 'Not in line with faculty policy.' } }]);
  });

  it('refreshes the schedule when the session changed meanwhile (409)', async () => {
    let changed = false;
    mockTutor({
      [`GET ${SCHEDULE}`]: () => json(200, page(changed ? [session({ status: 'COMPLETED' })] : [session()], false, 100)),
      [`POST ${CANCEL}`]: () => {
        changed = true;
        return json(409, { error: 'session cannot be cancelled in its current state' });
      },
    });
    renderApp(`/coordinator/tutors/${TUTOR_ID}`);
    const user = userEvent.setup();

    const entry = await scheduleEntry('Algorithms');
    await user.click(within(entry).getByRole('button', { name: /^Cancel session: Algorithms/ }));
    await user.type(within(entry).getByLabelText('Reason for cancelling'), 'Policy breach.');
    await user.click(within(entry).getByRole('button', { name: 'Cancel the session' }));

    expect(await screen.findByText(/This session can no longer be cancelled\. .* The schedule was refreshed\./)).toBeInTheDocument();
    const refreshed = await scheduleEntry('Algorithms');
    expect(await within(refreshed).findByText('Completed')).toBeInTheDocument();
    expect(within(refreshed).queryByRole('button')).not.toBeInTheDocument();
  });

  it('says when the tutor is not in the coordinator’s faculty', async () => {
    mockTutor({ [`GET ${TUTOR}`]: () => json(404, { error: 'tutor not found' }) });
    renderApp(`/coordinator/tutors/${TUTOR_ID}`);

    expect(await screen.findByText('Tutor not found')).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 1, name: 'Tutor' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '← All tutors' })).toHaveAttribute('href', '/coordinator/tutors');
  });
});
