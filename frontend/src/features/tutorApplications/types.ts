/**
 * Shapes of the tutor-application API, copied from the Go JSON tags in
 * backend/internal/tutoring/domain/tutoring.go and interfaces/http/handler.go.
 *
 * Privacy: student views never contain the grade. Only the coordinator
 * review detail (ApplicationReviewDetail) includes it.
 */

export type ApplicationStatus = 'PENDING' | 'APPROVED' | 'REJECTED';

/** Item of GET /tutor-applications/me and GET /coordinator/tutor-applications. */
export interface TutorApplication {
  id: string;
  /** The applicant's user ID. The API doesn't return the applicant's name. */
  student_id: string;
  subject_id: string;
  subject_name: string;
  status: ApplicationStatus;
  submitted_at: string;
  reviewed_at?: string;
  rejection_reason?: string;
}

/** GET /coordinator/tutor-applications/{id} → { data }. Coordinators of the applicant's faculty only. */
export interface ApplicationReviewDetail extends TutorApplication {
  /** Exact decimal string, for example "17.50". */
  grade: string;
  description: string;
}

/** Body of POST /tutor-applications. `grade` must be a JSON number (15–20, at most two decimals), not a string. */
export interface ApplicationSubmission {
  subject_id: string;
  grade: number;
  description: string;
}

/** 201 response of POST /tutor-applications. */
export interface ApplicationCreated {
  application_id: string;
  status: ApplicationStatus;
}
