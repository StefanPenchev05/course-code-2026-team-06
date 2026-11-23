import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../app/routes';
import { AuthProvider } from '../features/auth/AuthProvider';
import { json, mockApi, verifiedStudent } from '../test/mockApi';

// Component tests with a mocked API (test double). Real-backend checks are separate.

const FACULTY = { id: '00000000-0000-0000-0000-000000000101', name: 'Science, Technology and Medicine' };
const PROGRAM = {
  id: '00000000-0000-0000-0000-000000000111',
  faculty_id: FACULTY.id,
  name: 'Computing',
  level: 'BACHELOR',
};
const pendingStudent = { ...verifiedStudent, verified_student: false, verification_status: 'PENDING' };

const catalog = {
  'GET /api/v1/auth/me': () => json(401, { error: 'invalid credentials or session' }),
  'GET /api/v1/public/faculties': () => json(200, { data: [FACULTY] }),
  [`GET /api/v1/public/faculties/${FACULTY.id}/programs`]: () => json(200, { data: [PROGRAM] }),
};

function renderRegister() {
  const router = createMemoryRouter(routes, { initialEntries: ['/register'] });
  render(
    <AuthProvider>
      <RouterProvider router={router} />
    </AuthProvider>,
  );
  return router;
}

async function fillForm(user: ReturnType<typeof userEvent.setup>, overrides: { email?: string } = {}) {
  await user.type(await screen.findByLabelText('Full name'), 'Ana Student');
  await user.type(screen.getByLabelText(/University email/), overrides.email ?? 'ana@uni.lu');
  await user.type(screen.getByLabelText(/Student number/), '0123456789');
  await user.selectOptions(await screen.findByLabelText('Faculty'), FACULTY.id);
  await user.selectOptions(screen.getByLabelText('Study program'), await screen.findByRole('option', { name: 'Computing (Bachelor)' }));
  await user.type(screen.getByLabelText(/^Password/), 'a-long-test-password');
  await user.type(screen.getByLabelText('Repeat password'), 'a-long-test-password');
}

afterEach(() => vi.unstubAllGlobals());

describe('RegisterPage', () => {
  it('registers, logs the student in and shows the pending notice', async () => {
    const { calls } = mockApi({
      ...catalog,
      'POST /api/v1/auth/register': () => json(201, { verification_id: 'v1', verification_status: 'PENDING' }),
      'POST /api/v1/auth/login': () => json(200, pendingStudent),
    });
    const router = renderRegister();
    const user = userEvent.setup();

    await fillForm(user);
    await user.click(screen.getByRole('button', { name: 'Create account' }));

    expect(await screen.findByText('Your registration is waiting for approval')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/');
    expect(calls.find((c) => c.url.endsWith('/auth/register'))?.body).toEqual({
      name: 'Ana Student',
      email: 'ana@uni.lu',
      student_number: '0123456789',
      program_id: PROGRAM.id,
      password: 'a-long-test-password',
    });
  });

  it('checks the form before calling the API', async () => {
    const { calls } = mockApi(catalog);
    renderRegister();
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Create account' }));

    expect(screen.getByLabelText('Full name')).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByText('Choose your faculty.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('explains a rejected email domain next to the field and keeps the input', async () => {
    mockApi({
      ...catalog,
      'POST /api/v1/auth/register': () => json(422, { error: 'invalid input: use an allowed university email address' }),
    });
    renderRegister();
    const user = userEvent.setup();

    await fillForm(user, { email: 'ana@gmail.com' });
    await user.click(screen.getByRole('button', { name: 'Create account' }));

    expect(await screen.findByText(/Other email addresses can’t register/)).toBeInTheDocument();
    expect(screen.getByLabelText(/University email/)).toHaveValue('ana@gmail.com');
    expect(screen.getByLabelText('Full name')).toHaveValue('Ana Student');
  });

  it('offers to log in when the account already exists', async () => {
    mockApi({ ...catalog, 'POST /api/v1/auth/register': () => json(409, { error: 'conflicting operation' }) });
    renderRegister();
    const user = userEvent.setup();

    await fillForm(user);
    await user.click(screen.getByRole('button', { name: 'Create account' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('An account with this email or student number already exists.');
    expect(screen.getByRole('link', { name: 'Log in instead' })).toHaveAttribute('href', '/login');
    // The message is above the button, so focus moves to it.
    expect(alert.parentElement).toHaveFocus();
  });

  it('moves focus to the first invalid field', async () => {
    mockApi(catalog);
    renderRegister();
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('Full name'), 'Ana Student');
    await user.click(screen.getByRole('button', { name: 'Create account' }));

    expect(screen.getByLabelText(/University email/)).toHaveFocus();
  });

  it('asks for another program when the chosen one disappeared', async () => {
    mockApi({ ...catalog, 'POST /api/v1/auth/register': () => json(404, { error: 'resource not found' }) });
    renderRegister();
    const user = userEvent.setup();

    await fillForm(user);
    await user.click(screen.getByRole('button', { name: 'Create account' }));

    expect(await screen.findByText('This program is no longer available. Choose another one.')).toBeInTheDocument();
    expect(screen.getByLabelText('Study program')).toHaveValue('');
  });

  it('lets the student retry when the program list fails to load', async () => {
    let failures = 1;
    mockApi({
      ...catalog,
      'GET /api/v1/public/faculties': () => (failures-- > 0 ? json(500, { error: 'internal server error' }) : json(200, { data: [FACULTY] })),
    });
    renderRegister();
    const user = userEvent.setup();

    expect(await screen.findByText("We couldn't load the list of programs")).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Try again' }));

    expect(await screen.findByRole('option', { name: FACULTY.name })).toBeInTheDocument();
  });
});
