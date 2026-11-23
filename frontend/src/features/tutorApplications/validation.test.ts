import { describe, expect, it } from 'vitest';
import { byteLength, parseGrade, validateApplication, validateRejectionReason } from './validation';

describe('parseGrade', () => {
  it('accepts 15–20 with up to two decimals, and a decimal comma', () => {
    expect(parseGrade('17.5')).toBe(17.5);
    expect(parseGrade('17,25')).toBe(17.25);
    expect(parseGrade(' 20 ')).toBe(20);
    expect(parseGrade('15.00')).toBe(15);
  });

  it('rejects what the backend would reject', () => {
    expect(parseGrade('14.99')).toBeNull();
    expect(parseGrade('20.01')).toBeNull();
    expect(parseGrade('17.555')).toBeNull();
    expect(parseGrade('9')).toBeNull();
    expect(parseGrade('1e1')).toBeNull();
    expect(parseGrade('seventeen')).toBeNull();
    expect(parseGrade('')).toBeNull();
  });
});

describe('byteLength', () => {
  it('counts UTF-8 bytes like the backend', () => {
    expect(byteLength('abc')).toBe(3);
    expect(byteLength('é')).toBe(2);
    expect(byteLength('€')).toBe(3);
    expect(byteLength('😀')).toBe(4);
  });
});

describe('validateApplication', () => {
  it('requires a valid grade and a description', () => {
    expect(validateApplication('', '')).toEqual({
      grade: 'Enter your grade in this subject.',
      description: 'Tell the coordinator why you want to tutor this subject.',
    });
    expect(validateApplication('14', 'Hi').grade).toMatch(/from 15 to 20/);
    expect(validateApplication('18', '   ').description).toBeDefined();
    expect(validateApplication('18', 'I like helping.')).toEqual({});
  });

  it('limits the description to 4000 bytes, not characters', () => {
    expect(validateApplication('18', 'a'.repeat(4000)).description).toBeUndefined();
    expect(validateApplication('18', 'a'.repeat(4001)).description).toMatch(/too long/);
    expect(validateApplication('18', 'é'.repeat(2001)).description).toMatch(/too long/);
  });
});

describe('validateRejectionReason', () => {
  it('requires 1–2000 bytes after trimming', () => {
    expect(validateRejectionReason('  ')).toMatch(/Give a reason/);
    expect(validateRejectionReason('Grade not confirmed.')).toBeUndefined();
    expect(validateRejectionReason('a'.repeat(2001))).toMatch(/too long/);
  });
});
