import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, mockApi, verifiedStudent, type MockCall } from '../../test/mockApi';
import { page, PROGRAM_ID, renderApp } from './testUtils';

// TEST DOUBLE data (see src/test/mockApi.ts). These tests don't prove integration with the Go API.
const algorithms = { id: '22222222-2222-2222-2222-222222222222', code: 'BINFO-ALGO', name: 'Algorithms' };
const databases = { id: '33333333-3333-3333-3333-333333333333', code: 'BINFO-DB', name: 'Databases' };
// /auth/me returns the student's program, so SubjectSearch lists that program's subjects.
const student = { ...verifiedStudent, program_id: PROGRAM_ID };
const PROGRAM_SUBJECTS = `GET /api/v1/programs/${PROGRAM_ID}/subjects`;

function application(overrides: Record<string, unknown> = {}) {
  return {
    id: '66666666-6666-6666-6666-666666666666',
    student_id: student.id,
    subject_id: algorithms.id,
    subject_name: algorithms.name,
    status: 'PENDING',
    submitted_at: '2026-11-20T10:00:00Z',
    ...overrides,
  };
}

const GRADE_LABEL = 'Your grade in this subject (out of 20)';
const DESCRIPTION_LABEL = 'Why do you want to tutor this subject?';

async function chooseAlgorithms(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole('button', { name: /Algorithms/ }));
}

function posts(calls: MockCall[]) {
  return calls.filter((call) => call.method === 'POST');
}

afterEach(() => vi.unstubAllGlobals());

describe('tutor applications: student', () => {
  it('applies for a subject with the grade as a number, then shows it as pending', async () => {
    let applied = false;
    const { calls } = mockApi({
      'GET /api/v1/auth/me': () => json(200, student),
      'GET /api/v1/tutor-applications/me': () => json(200, page(applied ? [application()] : [])),
      [PROGRAM_SUBJECTS]: () => json(200, page([algorithms, databases])),
      'POST /api/v1/tutor-applications': () => {
        applied = true;
        return json(201, { application_id: '66666666-6666-6666-6666-666666666666', status: 'PENDING' });
      },
    });
    renderApp('/tutor-applications');
    const user = userEvent.setup();

    expect(await screen.findByText("You haven't applied to tutor a subject yet.")).toBeInTheDocument();
    await chooseAlgorithms(user);
    await user.type(screen.getByLabelText(GRADE_LABEL), '17,5');
    await user.type(screen.getByLabelText(DESCRIPTION_LABEL), '  I enjoy explaining recursion.  ');
    await user.click(screen.getByRole('button', { name: 'Send application' }));

    expect(await screen.findByText('Application sent')).toBeInTheDocument();
    expect(posts(calls)[0].body).toEqual({
      subject_id: algorithms.id,
      grade: 17.5,
      description: 'I enjoy explaining recursion.',
    });
    expect(calls.some((call) => call.url.startsWith(`/api/v1/programs/${PROGRAM_ID}/subjects`))).toBe(true);
    // The list is loaded again and now contains the pending application.
    expect(await screen.findByText('Waiting for review')).toBeInTheDocument();
  });

  it('shows decisions and the rejection reason', async () => {
    mockApi({
      'GET /api/v1/auth/me': () => json(200, student),
      'GET /api/v1/tutor-applications/me': () =>
        json(
          200,
          page([
            application({
              id: '77777777-7777-7777-7777-777777777777',
              subject_id: databases.id,
              subject_name: databases.name,
              status: 'REJECTED',
              reviewed_at: '2026-11-21T09:30:00Z',
              rejection_reason: 'Please apply again once your grade is confirmed.',
            }),
            application({ status: 'APPROVED', reviewed_at: '2026-11-19T08:00:00Z' }),
          ]),
        ),
    });
    renderApp('/tutor-applications');

    expect(await screen.findByRole('heading', { name: 'Databases' })).toBeInTheDocument();
    expect(screen.getByText('Not approved')).toBeInTheDocument();
    expect(screen.getByText('Please apply again once your grade is confirmed.')).toBeInTheDocument();
    expect(screen.getByText('21 Nov 2026, 10:30')).toBeInTheDocument();
    expect(screen.getByText('Approved')).toBeInTheDocument();
    expect(screen.getByText('This subject is now on your tutor profile.')).toBeInTheDocument();
    // No pending application, so the student can apply again.
    expect(screen.getByLabelText('Subject')).toBeInTheDocument();
  });

  it('replaces the form with a notice while an application is pending', async () => {
    mockApi({
      'GET /api/v1/auth/me': () => json(200, student),
      'GET /api/v1/tutor-applications/me': () => json(200, page([application()])),
    });
    renderApp('/tutor-applications');

    expect(await screen.findByText('You have an application waiting for review')).toBeInTheDocument();
    expect(screen.queryByLabelText('Subject')).not.toBeInTheDocument();
  });

  it('checks the grade and description before calling the API', async () => {
    const { calls } = mockApi({
      'GET /api/v1/auth/me': () => json(200, student),
      'GET /api/v1/tutor-applications/me': () => json(200, page([])),
      [PROGRAM_SUBJECTS]: () => json(200, page([algorithms])),
    });
    renderApp('/tutor-applications');
    const user = userEvent.setup();

    await chooseAlgorithms(user);
    await user.type(screen.getByLabelText(GRADE_LABEL), '14.5');
    await user.click(screen.getByRole('button', { name: 'Send application' }));

    expect(screen.getByLabelText(GRADE_LABEL)).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByText('Enter a grade from 15 to 20 with at most two decimals, for example 17.5.')).toBeInTheDocument();
    expect(screen.getByText('Tell the coordinator why you want to tutor this subject.')).toBeInTheDocument();
    expect(posts(calls)).toHaveLength(0);
  });

  it('explains a conflict and keeps what the student typed', async () => {
    mockApi({
      'GET /api/v1/auth/me': () => json(200, student),
      'GET /api/v1/tutor-applications/me': () => json(200, page([])),
      [PROGRAM_SUBJECTS]: () => json(200, page([algorithms])),
      'POST /api/v1/tutor-applications': () => json(409, { error: 'conflicting operation' }),
    });
    renderApp('/tutor-applications');
    const user = userEvent.setup();

    await chooseAlgorithms(user);
    await user.type(screen.getByLabelText(GRADE_LABEL), '18');
    await user.type(screen.getByLabelText(DESCRIPTION_LABEL), 'I tutored this last year.');
    await user.click(screen.getByRole('button', { name: 'Send application' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('You may already tutor it');
    expect(screen.getByLabelText(GRADE_LABEL)).toHaveValue('18');
  });

  it('explains when the subject is not in the student’s program', async () => {
    mockApi({
      'GET /api/v1/auth/me': () => json(200, student),
      'GET /api/v1/tutor-applications/me': () => json(200, page([])),
      [PROGRAM_SUBJECTS]: () => json(200, page([algorithms])),
      'POST /api/v1/tutor-applications': () =>
        json(422, { error: "invalid input: subject is not in the student's current program" }),
    });
    renderApp('/tutor-applications');
    const user = userEvent.setup();

    await chooseAlgorithms(user);
    await user.type(screen.getByLabelText(GRADE_LABEL), '18');
    await user.type(screen.getByLabelText(DESCRIPTION_LABEL), 'I like this subject.');
    await user.click(screen.getByRole('button', { name: 'Send application' }));

    expect(await screen.findByRole('alert')).toHaveTextContent("isn't part of your current study program");
  });
});
