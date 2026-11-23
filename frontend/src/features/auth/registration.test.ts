import { describe, expect, it } from 'vitest';
import { ApiError } from '../../shared/api/client';
import { byteLength, studentDetailsProblem, validateRegistration, type RegistrationForm } from './registration';

const valid: RegistrationForm = {
  name: 'Ana Student',
  email: 'ana@uni.lu',
  studentNumber: '0123456789',
  facultyId: 'f1',
  programId: 'p1',
  password: 'a-long-test-password',
  passwordConfirm: 'a-long-test-password',
};

describe('validateRegistration', () => {
  it('accepts a complete form', () => {
    expect(validateRegistration(valid)).toEqual({});
  });

  it('reports every missing field', () => {
    const errors = validateRegistration({ ...valid, name: ' ', email: '', studentNumber: '', facultyId: '', password: '' });
    expect(Object.keys(errors).sort()).toEqual(['email', 'facultyId', 'name', 'password', 'studentNumber']);
  });

  it('asks for a program once a faculty is chosen', () => {
    expect(validateRegistration({ ...valid, programId: '' }).programId).toBe('Choose your study program.');
  });

  it('mirrors the server password limits (12 to 256 bytes)', () => {
    expect(validateRegistration({ ...valid, password: 'short', passwordConfirm: 'short' }).password).toMatch(/at least 12/);
    const long = 'x'.repeat(257);
    expect(validateRegistration({ ...valid, password: long, passwordConfirm: long }).password).toMatch(/too long/);
  });

  it('counts bytes, not characters, like the server', () => {
    expect(byteLength('é')).toBe(2);
    const threeEmoji = '😀'.repeat(3); // 3 characters, 12 bytes
    expect(validateRegistration({ ...valid, password: threeEmoji, passwordConfirm: threeEmoji }).password).toBeUndefined();
  });

  it('checks that both passwords match', () => {
    expect(validateRegistration({ ...valid, passwordConfirm: 'something-else!' }).passwordConfirm).toBe("The passwords don't match.");
  });
});

describe('studentDetailsProblem', () => {
  it('puts the domain rule next to the email field', () => {
    const problem = studentDetailsProblem(new ApiError(422, 'invalid input: use an allowed university email address'), 'register');
    expect(problem.fields.email).toMatch(/university email/);
  });

  it('offers to log in when the account already exists', () => {
    const problem = studentDetailsProblem(new ApiError(409, 'conflicting operation'), 'register');
    expect(problem.alreadyRegistered).toBe(true);
    expect(problem.form).toMatch(/already exists/);
  });

  it('asks for another program when it disappeared', () => {
    expect(studentDetailsProblem(new ApiError(404, 'resource not found'), 'register').fields.programId).toMatch(/no longer available/);
  });

  it('shows other 422 rules without the "invalid input" prefix', () => {
    const problem = studentDetailsProblem(new ApiError(422, 'invalid input: provide a name, student ID and valid program ID'), 'register');
    expect(problem.form).toBe('Provide a name, student ID and valid program ID');
  });
});
