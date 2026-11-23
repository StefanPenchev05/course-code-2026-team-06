import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../../app/routes';
import { json, mockApi, verifiedStudent } from '../../test/mockApi';
import { AuthProvider } from './AuthProvider';

// Component tests with a mocked API (test double). Real-backend checks are separate.

const FACULTY = { id: '00000000-0000-0000-0000-000000000101', name: 'Demo Engineering' };
const PROGRAM = { id: '00000000-0000-0000-0000-000000000111', faculty_id: FACULTY.id, name: 'Computing', level: 'BACHELOR' };

const rejected = {
  ...verifiedStudent,
  verified_student: false,
  verification_status: 'REJECTED',
  rejection_reason: 'The student number does not match our records.',
};
const pending = { ...rejected, verification_status: 'PENDING', rejection_reason: undefined };

const catalog = {
  'GET /api/v1/public/faculties': () => json(200, { data: [FACULTY] }),
  [`GET /api/v1/public/faculties/${FACULTY.id}/programs`]: () => json(200, { data: [PROGRAM] }),
};

function renderHome() {
  render(
    <AuthProvider>
      <RouterProvider router={createMemoryRouter(routes, { initialEntries: ['/'] })} />
    </AuthProvider>,
  );
}

async function completeForm(user: ReturnType<typeof userEvent.setup>) {
  await user.type(await screen.findByLabelText(/Student number/), '9876543210');
  await user.selectOptions(await screen.findByLabelText('Faculty'), FACULTY.id);
  await user.selectOptions(screen.getByLabelText('Study program'), await screen.findByRole('option', { name: 'Computing (Bachelor)' }));
}

afterEach(() => vi.unstubAllGlobals());

describe('resubmitting a rejected registration', () => {
  it('shows the reason and pre-fills name and email', async () => {
    mockApi({ ...catalog, 'GET /api/v1/auth/me': () => json(200, rejected) });
    renderHome();

    expect(await screen.findByText('Reason: The student number does not match our records.')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Correct your details' })).toBeInTheDocument();
    expect(screen.getByLabelText('Full name')).toHaveValue('Ana Student');
    expect(screen.getByLabelText(/University email/)).toHaveValue('ana@uni.lu');
    expect(screen.getByLabelText(/Student number/)).toHaveValue('');
  });

  it('sends the corrected details and then shows the pending status', async () => {
    let me: typeof rejected | typeof pending = rejected;
    const { calls } = mockApi({
      ...catalog,
      'GET /api/v1/auth/me': () => json(200, me),
      'POST /api/v1/auth/resubmit-verification': () => {
        me = pending;
        return json(201, { verification_id: 'v2', verification_status: 'PENDING' });
      },
    });
    renderHome();
    const user = userEvent.setup();

    await completeForm(user);
    await user.click(screen.getByRole('button', { name: 'Send for approval again' }));

    expect(await screen.findByText('Your registration is waiting for approval')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Correct your details' })).not.toBeInTheDocument();
    expect(calls.find((c) => c.url.endsWith('/resubmit-verification'))?.body).toEqual({
      name: 'Ana Student',
      email: 'ana@uni.lu',
      student_number: '9876543210',
      program_id: PROGRAM.id,
    });
  });

  it('warns that a changed email becomes the login email', async () => {
    mockApi({ ...catalog, 'GET /api/v1/auth/me': () => json(200, rejected) });
    renderHome();
    const user = userEvent.setup();

    const email = await screen.findByLabelText(/University email/);
    await user.clear(email);
    await user.type(email, 'ana.student@uni.lu');

    expect(screen.getByText('You will log in with this new email address from now on.')).toBeInTheDocument();
  });

  it('explains a conflict and keeps the input', async () => {
    mockApi({
      ...catalog,
      'GET /api/v1/auth/me': () => json(200, rejected),
      'POST /api/v1/auth/resubmit-verification': () => json(409, { error: 'conflicting operation' }),
    });
    renderHome();
    const user = userEvent.setup();

    await completeForm(user);
    await user.click(screen.getByRole('button', { name: 'Send for approval again' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent(/couldn't be sent/);
    expect(alert.parentElement).toHaveFocus();
    expect(screen.getByLabelText(/Student number/)).toHaveValue('9876543210');
  });

  it('does not show the form to pending students', async () => {
    mockApi({ ...catalog, 'GET /api/v1/auth/me': () => json(200, pending) });
    renderHome();

    expect(await screen.findByText('Your registration is waiting for approval')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Correct your details' })).not.toBeInTheDocument();
  });
});
