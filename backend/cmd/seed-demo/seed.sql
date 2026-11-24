-- Fictional local demo only. The Go command owns the transaction and password hashing.
INSERT INTO faculties (id,name) VALUES
('f6000000-0000-4000-8000-000000000001','Tutti Demo Faculty');
INSERT INTO programs (id,faculty_id,name,level) VALUES
('f6000000-0000-4000-8000-000000000002','f6000000-0000-4000-8000-000000000001','Tutti Demo Computing','BACHELOR');
INSERT INTO subjects (id,code,name) VALUES
('f6000000-0000-4000-8000-000000000003','TUTTI-DEMO','Demo Software Engineering');
INSERT INTO program_subjects VALUES
('f6000000-0000-4000-8000-000000000002','f6000000-0000-4000-8000-000000000003');
INSERT INTO users (id,name,email,password_hash) VALUES
('f6000000-0000-4000-8000-000000000010','Demo Coordinator','coordinator@example.test',current_setting('tutti.demo_password_hash')),
('f6000000-0000-4000-8000-000000000011','Demo Student','student@example.test',current_setting('tutti.demo_password_hash')),
('f6000000-0000-4000-8000-000000000012','Demo Tutor','tutor@example.test',current_setting('tutti.demo_password_hash')),
('f6000000-0000-4000-8000-000000000013','Demo Pending Student','pending@example.test',current_setting('tutti.demo_password_hash'));
INSERT INTO coordinators (user_id,employee_number,faculty_id) VALUES
('f6000000-0000-4000-8000-000000000010','TUTTI-DEMO-C1','f6000000-0000-4000-8000-000000000001');
INSERT INTO students (user_id,student_number,program_id) VALUES
('f6000000-0000-4000-8000-000000000011','TUTTI-DEMO-S1','f6000000-0000-4000-8000-000000000002'),
('f6000000-0000-4000-8000-000000000012','TUTTI-DEMO-S2','f6000000-0000-4000-8000-000000000002'),
('f6000000-0000-4000-8000-000000000013','TUTTI-DEMO-S3','f6000000-0000-4000-8000-000000000002');
INSERT INTO student_verifications (student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id)
SELECT user_id,'f6000000-0000-4000-8000-000000000001','APPROVED',now(),'f6000000-0000-4000-8000-000000000010'
FROM students WHERE user_id IN ('f6000000-0000-4000-8000-000000000011','f6000000-0000-4000-8000-000000000012');
INSERT INTO student_verifications (student_id,faculty_id) VALUES
('f6000000-0000-4000-8000-000000000013','f6000000-0000-4000-8000-000000000001');
INSERT INTO tutor_profiles (student_id,description,approved_at) VALUES
('f6000000-0000-4000-8000-000000000012','Demo tutor for testing booking and teaching workflows.',now());
INSERT INTO tutor_applications (id,student_id,faculty_id,description,status,reviewed_at,reviewed_by_coordinator_id) VALUES
('f6000000-0000-4000-8000-000000000020','f6000000-0000-4000-8000-000000000012','f6000000-0000-4000-8000-000000000001','Demo qualification','APPROVED',now(),'f6000000-0000-4000-8000-000000000010');
INSERT INTO tutor_qualifications (id,application_id,subject_id,grade,student_id) VALUES
('f6000000-0000-4000-8000-000000000021','f6000000-0000-4000-8000-000000000020','f6000000-0000-4000-8000-000000000003',18.00,'f6000000-0000-4000-8000-000000000012');
INSERT INTO tutor_offerings (id,tutor_id,subject_id,qualification_id,description) VALUES
('f6000000-0000-4000-8000-000000000022','f6000000-0000-4000-8000-000000000012','f6000000-0000-4000-8000-000000000003','f6000000-0000-4000-8000-000000000021','Demo Software Engineering tutoring');
-- Reuse a real semester if one covers the next seven days; otherwise add a demo one.
INSERT INTO semesters (name,starts_at,ends_at)
SELECT 'Tutti local demo term',date_trunc('day',now()),date_trunc('day',now()) + interval '30 days'
WHERE NOT EXISTS (SELECT 1 FROM semesters WHERE starts_at <= now() AND ends_at >= now() + interval '8 days');
-- 09:00–17:00 Luxembourg local time, for the next seven calendar days.
INSERT INTO availability_slots (tutor_id,starts_at,ends_at,allowed_mode)
SELECT 'f6000000-0000-4000-8000-000000000012',
 ((now() AT TIME ZONE 'Europe/Luxembourg')::date + n + time '09:00') AT TIME ZONE 'Europe/Luxembourg',
 ((now() AT TIME ZONE 'Europe/Luxembourg')::date + n + time '17:00') AT TIME ZONE 'Europe/Luxembourg','BOTH'
FROM generate_series(1,7) AS n;
