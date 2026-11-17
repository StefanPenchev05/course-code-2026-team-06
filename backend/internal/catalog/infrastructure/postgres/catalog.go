package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
)

type database interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct {
	db      database
	rowLock string
}

func New(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

// InTransaction keeps program and subject-membership reads stable until commit.
func InTransaction(tx pgx.Tx) *Repository {
	return &Repository{db: tx, rowLock: " FOR SHARE"}
}

func (r *Repository) ListFaculties(ctx context.Context) ([]domain.Faculty, error) {
	rows, err := r.db.Query(ctx, "SELECT id::text, name FROM faculties ORDER BY name, id")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[domain.Faculty])
}

func (r *Repository) ListPrograms(ctx context.Context, facultyID string, level domain.DegreeLevel) ([]domain.Program, error) {
	// A left join keeps an existing faculty visible when the filter yields no programs.
	rows, err := r.db.Query(ctx, `
		SELECT p.id::text, f.id::text, coalesce(p.name, ''), coalesce(p.level::text, '')
		FROM faculties f
		LEFT JOIN programs p ON p.faculty_id = f.id
			AND (NULLIF($2::text, '') IS NULL OR p.level = NULLIF($2::text, '')::degree_level)
		WHERE f.id = $1
		ORDER BY p.name, p.id`, facultyID, level)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	programs := make([]domain.Program, 0)
	found := false
	for rows.Next() {
		found = true
		var id *string
		var program domain.Program
		if err := rows.Scan(&id, &program.FacultyID, &program.Name, &program.Level); err != nil {
			return nil, err
		}
		if id != nil {
			program.ID = *id
			programs = append(programs, program)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, domain.ErrFacultyNotFound
	}
	return programs, nil
}

func (r *Repository) GetProgram(ctx context.Context, id string) (domain.ProgramDetails, error) {
	var p domain.ProgramDetails
	err := r.db.QueryRow(ctx, `
		SELECT p.id::text, p.faculty_id::text, p.name, p.level::text, f.name
		FROM programs p JOIN faculties f ON f.id = p.faculty_id
		WHERE p.id = $1`, id).Scan(&p.ID, &p.FacultyID, &p.Name, &p.Level, &p.Faculty.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrProgramNotFound
	}
	p.Faculty.ID = p.FacultyID
	return p, err
}

const subjectMatches = `(strpos(lower(s.name), lower($1)) > 0 OR strpos(lower(s.code), lower($1)) > 0)`

func (r *Repository) SearchSubjects(ctx context.Context, query string, limit, offset int) ([]domain.Subject, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.id::text, s.code, s.name FROM subjects s
		WHERE `+subjectMatches+`
		ORDER BY s.name, s.code, s.id LIMIT $2 OFFSET $3`, query, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[domain.Subject])
}

func (r *Repository) ListProgramSubjects(ctx context.Context, programID, query string, limit, offset int) ([]domain.Subject, error) {
	// The lateral join preserves the program even when it has no matching page.
	rows, err := r.db.Query(ctx, `
		SELECT page.id::text, coalesce(page.code, ''), coalesce(page.name, '')
		FROM programs p
		LEFT JOIN LATERAL (
			SELECT s.id, s.code, s.name
			FROM program_subjects ps JOIN subjects s ON s.id = ps.subject_id
			WHERE ps.program_id = p.id AND `+subjectMatches+`
			ORDER BY s.name, s.code, s.id LIMIT $2 OFFSET $3
		) page ON true
		WHERE p.id = $4
		ORDER BY page.name, page.code, page.id`, query, limit, offset, programID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	subjects := make([]domain.Subject, 0)
	found := false
	for rows.Next() {
		found = true
		var id *string
		var subject domain.Subject
		if err := rows.Scan(&id, &subject.Code, &subject.Name); err != nil {
			return nil, err
		}
		if id != nil {
			subject.ID = *id
			subjects = append(subjects, subject)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, domain.ErrProgramNotFound
	}
	return subjects, nil
}

func (r *Repository) GetSubject(ctx context.Context, id string) (domain.Subject, error) {
	var s domain.Subject
	err := r.db.QueryRow(ctx, "SELECT id::text, code, name FROM subjects WHERE id = $1", id).
		Scan(&s.ID, &s.Code, &s.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrSubjectNotFound
	}
	return s, err
}

func (r *Repository) GetProgramFaculty(ctx context.Context, id string) (string, error) {
	var faculty string
	err := r.db.QueryRow(ctx, "SELECT faculty_id::text FROM programs WHERE id = $1"+r.rowLock, id).Scan(&faculty)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrProgramNotFound
	}
	return faculty, err
}

func (r *Repository) SubjectBelongsToProgram(ctx context.Context, programID, subjectID string) (bool, error) {
	var belongs bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM program_subjects WHERE program_id = $1 AND subject_id = $2`+r.rowLock+`)`,
		programID, subjectID).Scan(&belongs)
	return belongs, err
}
