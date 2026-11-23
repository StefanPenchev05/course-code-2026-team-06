package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
)

var _ application.RealtimeQueries = (*BookingRepository)(nil)

func (r *BookingRepository) BookingAudience(ctx context.Context,id string)(application.BookingAudience,error){
	var a application.BookingAudience
	err:=r.db.QueryRow(ctx,`SELECT s.student_id::text,s.tutor_id::text,s.faculty_id::text,coalesce(c.user_id::text,'')
		FROM tutoring_sessions s LEFT JOIN coordinators c ON c.faculty_id=s.faculty_id WHERE s.id=$1`,id).
		Scan(&a.StudentID,&a.TutorID,&a.FacultyID,&a.CoordinatorID)
	if errors.Is(err,pgx.ErrNoRows){ return a,nil }
	return a,err
}
