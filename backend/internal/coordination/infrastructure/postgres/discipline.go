package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
)

type discipline struct{ tx pgx.Tx }

func Discipline(tx pgx.Tx) application.Discipline { return discipline{tx} }

func (d discipline) RecordLateCancellation(ctx context.Context, v application.LateCancellation) error {
	_, err := d.tx.Exec(ctx, `
		INSERT INTO user_violations (user_id,semester_id,session_id,kind,occurred_at)
		VALUES ($1,$2,$3,'LATE_CANCELLATION',$4)
		ON CONFLICT (user_id,session_id) DO NOTHING`, v.UserID, v.SemesterID, v.SessionID, v.OccurredAt)
	if err != nil {
		return err
	}
	_, err = d.tx.Exec(ctx, `
		WITH flagged AS (
			INSERT INTO user_semester_flags (user_id,semester_id,faculty_id,flagged_at)
			SELECT $1,$2,$3,$4
			WHERE (SELECT count(*) FROM user_violations WHERE user_id=$1 AND semester_id=$2)>=2
			ON CONFLICT (user_id,semester_id) DO NOTHING
			RETURNING id,faculty_id
		)
		INSERT INTO notifications (recipient_user_id,type,subject,body,flag_id,deduplication_key)
		SELECT c.user_id,'USER_FLAGGED','Semester violation threshold reached',
		       'Sign in to Tutti to review the semester flag.',f.id,'semester-flag:'||f.id::text
		FROM flagged f JOIN coordinators c ON c.faculty_id=f.faculty_id`,
		v.UserID, v.SemesterID, v.HomeFacultyID, v.OccurredAt)
	return err
}
