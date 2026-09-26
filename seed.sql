-- Run after schema.sql. PostgreSQL generates every new row's UUID.
-- All people, records, reports and measurements below are fictional demo data.
-- Existing profiles are resolved by case-insensitive email; their IDs are preserved.
-- Child fixtures are matched by their semantic payload, deliberately excluding time.
-- Unchanged reruns preserve rows/timestamps; an edited payload can add a new row.
BEGIN;

-- Both seed scripts use this transaction-scoped lock to serialize their reruns.
SELECT pg_advisory_xact_lock(1129071442, 1);

INSERT INTO public.profiles (role, name, email, license_num, specialization, dob, blood_type) VALUES
('doctor', 'Dr. Sarah Chen', 'sarah.chen@example.com', 'DEMO-ORTHO-2048', 'Orthopedics', NULL, NULL),
('doctor', 'Dr. James Wilson', 'james.wilson@example.com', 'DEMO-PCP-1024', 'Primary care', NULL, NULL),
('patient', 'Alex Morgan', 'alex.morgan@example.com', NULL, NULL, '1994-06-15', 'O+'),
('patient', 'Jordan Lee', 'jordan.lee@example.com', NULL, NULL, '1987-03-22', 'A+'),
('patient', 'Taylor Brooks', 'taylor.brooks@example.com', NULL, NULL, '2000-11-08', 'B+')
ON CONFLICT (lower(email)) DO NOTHING;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM (VALUES
            ('sarah.chen@example.com', 'doctor'),
            ('james.wilson@example.com', 'doctor'),
            ('alex.morgan@example.com', 'patient'),
            ('jordan.lee@example.com', 'patient'),
            ('taylor.brooks@example.com', 'patient')
        ) AS expected(email, role)
        LEFT JOIN public.profiles p ON lower(p.email) = expected.email
        WHERE p.id IS NULL OR p.role <> expected.role
    ) THEN
        RAISE EXCEPTION 'A base fixture email is missing or belongs to the wrong profile role; no seed changes were committed.';
    END IF;
END;
$$;

WITH fixture(patient_email, doctor_email) AS (VALUES
('alex.morgan@example.com', 'sarah.chen@example.com'),
('alex.morgan@example.com', 'james.wilson@example.com'),
('jordan.lee@example.com', 'sarah.chen@example.com'),
('taylor.brooks@example.com', 'james.wilson@example.com')
)
INSERT INTO public.care_team (patient_id, doctor_id)
SELECT p.id, d.id
FROM fixture f
JOIN public.profiles p ON lower(p.email) = f.patient_email
JOIN public.profiles d ON lower(d.email) = f.doctor_email
ON CONFLICT (patient_id, doctor_id) DO NOTHING;

WITH fixture(patient_email, doctor_email, type, content, timestamp, image_url) AS (VALUES
('alex.morgan@example.com', 'sarah.chen@example.com', 'note', 'Two-week knee recovery review: incision is healing well. Swelling has reduced and range of motion is improving. Continue the rehabilitation plan and follow up in two weeks.', current_timestamp - interval '2 hours', NULL),
('alex.morgan@example.com', 'sarah.chen@example.com', 'prescription', 'Demo medication plan: acetaminophen as directed on the discharge instructions, only when needed. Review all medications with your care team.', current_timestamp - interval '26 hours', NULL),
('alex.morgan@example.com', 'sarah.chen@example.com', 'imaging', 'Follow-up knee imaging: postoperative alignment is maintained. No new concerning findings in this simulated study.', current_timestamp - interval '50 hours', '/static/ct-scan.svg'),
('alex.morgan@example.com', 'james.wilson@example.com', 'note', 'Recovery check-in: sleep is improving and Alex is walking more comfortably with support. Keep sharing any changes with the care team.', current_timestamp - interval '74 hours', NULL),
('alex.morgan@example.com', 'sarah.chen@example.com', 'note', 'Initial postoperative review: begin the agreed gentle movement plan with your physiotherapist. Expected swelling is present; the wound looks clean.', current_timestamp - interval '170 hours', NULL),
('jordan.lee@example.com', 'sarah.chen@example.com', 'note', 'Shoulder follow-up: mobility is improving with physiotherapy. Continue the agreed exercise plan and review next month.', current_timestamp - interval '5 hours', NULL),
('jordan.lee@example.com', 'sarah.chen@example.com', 'prescription', 'Demo prescription review: continue current care plan. Medication questions will be reviewed at the next appointment.', current_timestamp - interval '52 hours', NULL),
('jordan.lee@example.com', 'sarah.chen@example.com', 'imaging', 'Simulated shoulder imaging reviewed. Findings are consistent with the established recovery plan.', current_timestamp - interval '100 hours', '/static/ct-scan.svg'),
('taylor.brooks@example.com', 'james.wilson@example.com', 'note', 'Ankle recovery check: less swelling reported and daily activity is gradually increasing. Follow up as scheduled.', current_timestamp - interval '8 hours', NULL),
('taylor.brooks@example.com', 'james.wilson@example.com', 'prescription', 'Demo medication reconciliation completed. No changes to the discharge medication plan.', current_timestamp - interval '76 hours', NULL),
('taylor.brooks@example.com', 'james.wilson@example.com', 'imaging', 'Simulated ankle imaging reviewed with the patient. Recovery remains on the expected course.', current_timestamp - interval '124 hours', '/static/ct-scan.svg')
)
INSERT INTO public.medical_records (patient_id, doctor_id, type, content, timestamp, image_url)
SELECT p.id, d.id, f.type, f.content, f.timestamp, f.image_url
FROM fixture f
JOIN public.profiles p ON lower(p.email) = f.patient_email
JOIN public.profiles d ON lower(d.email) = f.doctor_email
WHERE NOT EXISTS (
    SELECT 1 FROM public.medical_records r
    WHERE r.patient_id = p.id AND r.doctor_id = d.id
      AND r.type = f.type AND r.content = f.content
      AND r.image_url IS NOT DISTINCT FROM f.image_url
);

WITH fixture(patient_email, file_name, timestamp) AS (VALUES
('alex.morgan@example.com', 'physiotherapy-progress.pdf', current_timestamp - interval '4 hours'),
('alex.morgan@example.com', 'discharge-summary.pdf', current_timestamp - interval '168 hours'),
('jordan.lee@example.com', 'shoulder-exercises.pdf', current_timestamp - interval '12 hours'),
('taylor.brooks@example.com', 'ankle-recovery-notes.pdf', current_timestamp - interval '20 hours')
)
INSERT INTO public.patient_uploads (patient_id, file_name, timestamp)
SELECT p.id, f.file_name, f.timestamp
FROM fixture f
JOIN public.profiles p ON lower(p.email) = f.patient_email
WHERE NOT EXISTS (
    SELECT 1 FROM public.patient_uploads u
    WHERE u.patient_id = p.id AND u.file_name = f.file_name
);

WITH fixture(patient_email, status_type, value, timestamp) AS (VALUES
('alex.morgan@example.com', 'pain', 6, current_timestamp - interval '168 hours'),
('alex.morgan@example.com', 'mobility', 3, current_timestamp - interval '168 hours'),
('alex.morgan@example.com', 'energy', 4, current_timestamp - interval '168 hours'),
('alex.morgan@example.com', 'pain', 3, current_timestamp - interval '3 hours'),
('alex.morgan@example.com', 'mobility', 7, current_timestamp - interval '3 hours'),
('alex.morgan@example.com', 'energy', 8, current_timestamp - interval '3 hours'),
('jordan.lee@example.com', 'pain', 2, current_timestamp - interval '6 hours'),
('jordan.lee@example.com', 'mobility', 8, current_timestamp - interval '6 hours'),
('jordan.lee@example.com', 'energy', 7, current_timestamp - interval '6 hours'),
('taylor.brooks@example.com', 'pain', 4, current_timestamp - interval '9 hours'),
('taylor.brooks@example.com', 'mobility', 6, current_timestamp - interval '9 hours'),
('taylor.brooks@example.com', 'energy', 7, current_timestamp - interval '9 hours')
)
INSERT INTO public.healing_progress (patient_id, status_type, value, timestamp)
SELECT p.id, f.status_type, f.value, f.timestamp
FROM fixture f
JOIN public.profiles p ON lower(p.email) = f.patient_email
WHERE NOT EXISTS (
    SELECT 1 FROM public.healing_progress h
    WHERE h.patient_id = p.id AND h.status_type = f.status_type AND h.value = f.value
);

WITH fixture(doctor_email, heart_rate, sleep_hours, timestamp) AS (VALUES
('sarah.chen@example.com', 64, 7.60, current_timestamp - interval '30 minutes'),
('james.wilson@example.com', 68, 7.20, current_timestamp - interval '45 minutes')
)
INSERT INTO public.mock_biometric_data (doctor_id, heart_rate, sleep_hours, timestamp)
SELECT d.id, f.heart_rate, f.sleep_hours, f.timestamp
FROM fixture f
JOIN public.profiles d ON lower(d.email) = f.doctor_email
WHERE NOT EXISTS (
    SELECT 1 FROM public.mock_biometric_data b
    WHERE b.doctor_id = d.id AND b.heart_rate = f.heart_rate AND b.sleep_hours = f.sleep_hours
);

COMMIT;
