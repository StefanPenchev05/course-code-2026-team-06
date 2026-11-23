import { ProgramPicker } from '../catalog';
import { TextField } from '../../shared/ui';
import { LIMITS, type FieldErrors, type StudentDetailsForm } from './registration';

interface StudentDetailsFieldsProps {
  values: StudentDetailsForm;
  errors: FieldErrors<StudentDetailsForm>;
  onChange: (field: keyof StudentDetailsForm, value: string) => void;
  disabled?: boolean;
  emailHint?: string;
}

/** Name, email, student number and program: the fields registration and resubmission share. */
export function StudentDetailsFields({
  values,
  errors,
  onChange,
  disabled,
  emailHint = 'Use the email address your university gave you.',
}: StudentDetailsFieldsProps) {
  return (
    <>
      <TextField
        label="Full name"
        name="name"
        autoComplete="name"
        value={values.name}
        onChange={(event) => onChange('name', event.target.value)}
        error={errors.name}
        maxLength={LIMITS.name}
        disabled={disabled}
      />
      <TextField
        label="University email"
        type="email"
        name="email"
        autoComplete="email"
        hint={emailHint}
        value={values.email}
        onChange={(event) => onChange('email', event.target.value)}
        error={errors.email}
        maxLength={LIMITS.email}
        disabled={disabled}
      />
      <TextField
        label="Student number"
        name="student_number"
        autoComplete="off"
        hint="As shown on your student card. A coordinator checks it before you can book tutors."
        value={values.studentNumber}
        onChange={(event) => onChange('studentNumber', event.target.value)}
        error={errors.studentNumber}
        maxLength={LIMITS.studentNumber}
        disabled={disabled}
      />
      <ProgramPicker
        facultyId={values.facultyId}
        programId={values.programId}
        onFacultyChange={(value) => onChange('facultyId', value)}
        onProgramChange={(value) => onChange('programId', value)}
        facultyError={errors.facultyId}
        programError={errors.programId}
        disabled={disabled}
      />
    </>
  );
}
