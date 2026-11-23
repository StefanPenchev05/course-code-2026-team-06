import { expect, request, type APIRequestContext } from '@playwright/test';
import { E2E, newPassword, testCatalog, uniqueEmail, uniqueStudentNumber } from './env';

/**
 * Setup steps done through the REAL Go API (as the coordinator and tutor
 * screens of later issues will do them), so a journey can start from a state
 * such as "an approved tutor with free time" without clicking through
 * screens that belong to other issues.
 */

export interface Student {
  name: string;
  email: string;
  studentNumber: string;
  password: string;
}

export function newStudent(prefix: string): Student {
  return { name: `E2E ${prefix}`, email: uniqueEmail(prefix), studentNumber: uniqueStudentNumber(), password: newPassword() };
}

/** A logged-in API session. Each call keeps its own cookie, like a separate browser. */
export async function loggedIn(email: string, password: string): Promise<APIRequestContext> {
  const api = await request.newContext({ baseURL: E2E.baseURL, extraHTTPHeaders: { Origin: E2E.baseURL } });
  const login = await api.post('/api/v1/auth/login', { data: { email, password } });
  expect(login.status(), `login of ${email}`).toBe(200);
  return api;
}

async function asCoordinator<T>(action: (api: APIRequestContext) => Promise<T>): Promise<T> {
  const { coordinatorEmail, coordinatorPassword } = testCatalog();
  const api = await loggedIn(coordinatorEmail, coordinatorPassword);
  try {
    return await action(api);
  } finally {
    await api.dispose();
  }
}

async function pendingVerificationId(api: APIRequestContext, studentEmail: string): Promise<string> {
  const queue = (await (await api.get('/api/v1/coordinator/verifications')).json()) as { id: string; email: string }[];
  const verification = queue.find((item) => item.email === studentEmail);
  expect(verification, `registration of ${studentEmail} is in the coordinator queue`).toBeDefined();
  return verification!.id;
}

export async function registerViaApi(student: Student): Promise<void> {
  const api = await request.newContext({ baseURL: E2E.baseURL, extraHTTPHeaders: { Origin: E2E.baseURL } });
  try {
    const response = await api.post('/api/v1/auth/register', {
      data: {
        name: student.name,
        email: student.email,
        student_number: student.studentNumber,
        program_id: testCatalog().programId,
        password: student.password,
      },
    });
    expect(response.status(), `registration of ${student.email}`).toBe(201);
  } finally {
    await api.dispose();
  }
}

export function approveRegistration(studentEmail: string): Promise<void> {
  return asCoordinator(async (api) => {
    const id = await pendingVerificationId(api, studentEmail);
    expect((await api.post(`/api/v1/coordinator/verifications/${id}/approve`)).status()).toBe(204);
  });
}

export function rejectRegistration(studentEmail: string, reason: string): Promise<void> {
  return asCoordinator(async (api) => {
    const id = await pendingVerificationId(api, studentEmail);
    expect((await api.post(`/api/v1/coordinator/verifications/${id}/reject`, { data: { reason } })).status()).toBe(204);
  });
}

/** A student who is registered and approved by the coordinator. */
export async function verifiedStudent(prefix: string): Promise<Student> {
  const student = newStudent(prefix);
  await registerViaApi(student);
  await approveRegistration(student.email);
  return student;
}

export interface FreeWindow {
  startsAt: Date;
  endsAt: Date;
}

/** An approved tutor for this run's subject: application with a private grade, approved by the coordinator. */
export async function approvedTutor(prefix: string, description: string, grade: number): Promise<Student> {
  const tutor = await verifiedStudent(prefix);
  const api = await loggedIn(tutor.email, tutor.password);
  try {
    const applied = await api.post('/api/v1/tutor-applications', {
      data: { subject_id: testCatalog().subjectId, grade, description },
    });
    expect(applied.status(), 'tutor application').toBe(201);
    const { application_id: applicationId } = (await applied.json()) as { application_id: string };

    await asCoordinator(async (coordinator) => {
      const approved = await coordinator.post(`/api/v1/coordinator/tutor-applications/${applicationId}/approve`);
      expect(approved.status(), 'tutor application approval').toBe(204);
    });
  } finally {
    await api.dispose();
  }
  return tutor;
}

/** An approved tutor who published one free window (the tutor's own POST /me/availability/slots). */
export async function approvedTutorWithFreeTime(
  prefix: string,
  description: string,
  grade: number,
  window: FreeWindow,
): Promise<Student> {
  const tutor = await approvedTutor(prefix, description, grade);
  const api = await loggedIn(tutor.email, tutor.password);
  try {
    const slot = await api.post('/api/v1/tutors/me/availability/slots', {
      data: { starts_at: window.startsAt.toISOString(), ends_at: window.endsAt.toISOString(), allowed_mode: 'ONLINE' },
    });
    expect(slot.status(), `free window: ${await slot.text()}`).toBe(201);
  } finally {
    await api.dispose();
  }
  return tutor;
}

/**
 * A verified student requests a 60-minute session at the start of one of the
 * tutor's windows (real POST /sessions). The REQUESTED session then reserves that time.
 */
export async function bookWindow(learner: Student, tutorId: string, windowId: string, startsAt: string): Promise<void> {
  const api = await loggedIn(learner.email, learner.password);
  try {
    const profile = (await (await api.get(`/api/v1/tutors/${tutorId}`)).json()) as { data: { offerings: { id: string }[] } };
    const booked = await api.post('/api/v1/sessions', {
      data: {
        offering_id: profile.data.offerings[0].id,
        availability_slot_id: windowId,
        starts_at: startsAt,
        duration_minutes: 60,
        mode: 'ONLINE',
        purpose: 'EXAM_PREPARATION',
        description: 'E2E booking that protects this window.',
      },
    });
    expect(booked.status(), `booking: ${await booked.text()}`).toBe(201);
  } finally {
    await api.dispose();
  }
}

/** The home-faculty coordinator suspends a user (POST /coordinator/users/{id}/suspend, returns 200). */
export function suspendUser(userId: string, reason: string): Promise<void> {
  return asCoordinator(async (api) => {
    const suspended = await api.post(`/api/v1/coordinator/users/${userId}/suspend`, { data: { reason } });
    expect(suspended.status(), `suspension: ${await suspended.text()}`).toBe(200);
  });
}
