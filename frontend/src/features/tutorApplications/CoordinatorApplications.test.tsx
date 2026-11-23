import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, mockApi, verifiedStudent } from '../../test/mockApi';
import { coordinatorUser, page, renderApp } from './testUtils';

// TEST DOUBLE data (see src/test/mockApi.ts). These tests don't prove integration with the Go API.
const APP_ID = '66666666-6666-6666-6666-666666666666';
const DETAIL_PATH = `/api/v1/coordinator/tutor-applications/${APP_ID}`;

const pending = {
  id: APP_ID,
  student_id: 'abcdef12-3456-7890-abcd-ef1234567890',
  subject_id: '22222222-2222-2222-2222-222222222222',
  subject_name: 'Algorithms',
  status: 'PENDING',
  submitted_at: '2026-11-20T10:00:00Z',
};
const detail = { ...pending, grade: '17.50', description: 'I passed with 17.5 and like helping first-year students.' };

afterEach(() => vi.unstubAllGlobals());

describe('tutor applications: coordinator', () => {
  it('lists pending applications by default and filters by status', async () => {
    const { calls } = mockApi({
      'GET /api/v1/auth/me': () => json(200, coordinatorUser),
      'GET /api/v1/coordinator/tutor-applications': (call) =>
        json(200, page(call.url.includes('status=REJECTED') ? [] : [pending])),
    });
    const router = renderApp('/coordinator/tutor-applications');
    const user = userEvent.setup();

    expect(await screen.findByRole('link', { name: 'Algorithms' })).toHaveAttribute(
      'href',
      `/coordinator/tutor-applications/${APP_ID}`,
    );
    expect(screen.getByText('ABCDEF12')).toBeInTheDocument();
    expect(calls.find((call) => call.url.startsWith('/api/v1/coordinator/'))?.url).toContain('status=PENDING');

    await user.selectOptions(screen.getByLabelText('Show applications'), 'REJECTED');

    expect(await screen.findByText('No rejected applications.')).toBeInTheDocument();
    expect(router.state.location.search).toBe('?status=REJECTED');
  });

  it('shows the private grade and approves only after confirmation', async () => {
    let approved = false;
    const { calls } = mockApi({
      'GET /api/v1/auth/me': () => json(200, coordinatorUser),
      [`GET ${DETAIL_PATH}`]: () =>
        json(200, { data: approved ? { ...detail, status: 'APPROVED', reviewed_at: '2026-11-22T08:00:00Z' } : detail }),
      [`POST ${DETAIL_PATH}/approve`]: () => {
        approved = true;
        return json(204);
      },
    });
    renderApp(`/coordinator/tutor-applications/${APP_ID}`);
    const user = userEvent.setup();

    expect(await screen.findByRole('heading', { name: 'Application to tutor Algorithms' })).toBeInTheDocument();
    expect(screen.getByText('17.50 / 20')).toBeInTheDocument();
    expect(screen.getByText(detail.description)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Approve' }));
    expect(calls.some((call) => call.method === 'POST')).toBe(false);
    await user.click(screen.getByRole('button', { name: 'Yes, approve' }));

    expect(await screen.findByText(/^Application approved/)).toBeInTheDocument();
    expect(await screen.findByText('Approved')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Reject' })).not.toBeInTheDocument();
  });

  it('requires a reason before rejecting and sends it', async () => {
    const { calls } = mockApi({
      'GET /api/v1/auth/me': () => json(200, coordinatorUser),
      [`GET ${DETAIL_PATH}`]: () => json(200, { data: detail }),
      [`POST ${DETAIL_PATH}/reject`]: () => json(204),
    });
    renderApp(`/coordinator/tutor-applications/${APP_ID}`);
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Reject' }));
    await user.click(screen.getByRole('button', { name: 'Reject application' }));

    expect(screen.getByText('Give a reason. The student will see it.')).toBeInTheDocument();
    expect(calls.some((call) => call.method === 'POST')).toBe(false);

    await user.type(screen.getByLabelText('Reason for rejecting'), 'The grade is from a different course.');
    await user.click(screen.getByRole('button', { name: 'Reject application' }));

    expect(await screen.findByText(/^Application rejected/)).toBeInTheDocument();
    expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ reason: 'The grade is from a different course.' });
  });

  it('explains an already-decided application and shows its current state', async () => {
    let reads = 0;
    mockApi({
      'GET /api/v1/auth/me': () => json(200, coordinatorUser),
      [`GET ${DETAIL_PATH}`]: () => {
        reads += 1;
        return json(200, {
          data:
            reads === 1
              ? detail
              : { ...detail, status: 'REJECTED', reviewed_at: '2026-11-22T08:00:00Z', rejection_reason: 'Decided by a colleague.' },
        });
      },
      [`POST ${DETAIL_PATH}/approve`]: () => json(409, { error: 'conflicting operation' }),
    });
    renderApp(`/coordinator/tutor-applications/${APP_ID}`);
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Approve' }));
    await user.click(screen.getByRole('button', { name: 'Yes, approve' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('already decided');
    expect(await screen.findByText('Decided by a colleague.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Approve' })).not.toBeInTheDocument();
  });

  it('does not reveal applications from another faculty', async () => {
    mockApi({
      'GET /api/v1/auth/me': () => json(200, coordinatorUser),
      [`GET ${DETAIL_PATH}`]: () => json(404, { error: 'resource not found' }),
    });
    renderApp(`/coordinator/tutor-applications/${APP_ID}`);

    expect(await screen.findByRole('alert')).toHaveTextContent("doesn't exist or belongs to another faculty");
  });

  it('hides the review queue from students', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    renderApp('/coordinator/tutor-applications');

    expect(await screen.findByRole('heading', { name: "You don't have access to this page" })).toBeInTheDocument();
  });
});
