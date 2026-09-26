-- Optional large ClearChart fixture: run AFTER schema.sql and seed.sql.
-- Adds 300 fictional patients, 1,500 records (including 600 image references),
-- 300 filename-only uploads, 900 healing scores and 400 care-team links.
-- PostgreSQL generates every new row's UUID; fixture emails resolve existing IDs.
-- Child fixtures are matched by semantic payload, deliberately excluding time.
-- Unchanged reruns preserve rows/timestamps; an edited payload can add a new row.
BEGIN;

-- Matches seed.sql; concurrent runs of either script wait for the other commit.
SELECT pg_advisory_xact_lock(1129071442, 1);

DO $$
BEGIN
    IF (SELECT count(*) FROM public.profiles
        WHERE lower(email) IN ('sarah.chen@example.com', 'james.wilson@example.com')
        AND role = 'doctor') <> 2 THEN
        RAISE EXCEPTION 'Run schema.sql and seed.sql first: both fixture doctor emails must exist with doctor role.';
    END IF;
END;
$$;
CREATE TEMP TABLE clearchart_bulk_seed ON COMMIT DROP AS
WITH names AS (
    SELECT i,
        (ARRAY['Avery','Bailey','Cameron','Casey','Dakota','Drew','Elliot','Emery','Finley','Harper','Jamie','Jesse','Kai','Logan','Morgan','Parker','Quinn','Riley','Rowan','Zoe'])[((i-1)/15)+1] AS first_name,
        (ARRAY['Bennett','Carter','Diaz','Ellis','Foster','Garcia','Hayes','Ibrahim','Jensen','Kim','Lopez','Nguyen','Patel','Rivera','Thompson'])[((i-1)%15)+1] AS last_name
    FROM generate_series(1,300) AS g(i)
), scenarios (n, area, review, baseline, medication) AS (
    VALUES
    (0, 'knee', 'Walking confidence is improving and reported swelling has eased. Physiotherapy progress was reviewed.', 'Initial knee recovery discussion documented stiffness and a need for support on longer walks.', 'Fictional acetaminophen review recorded; the existing discharge plan is unchanged.'),
    (1, 'shoulder', 'Comfort during daily activities has improved. Range-of-motion progress was reviewed with the therapy team.', 'Initial shoulder review documented difficulty reaching overhead and interrupted sleep.', 'Fictional anti-inflammatory medication review recorded; questions are reserved for the care team.'),
    (2, 'ankle', 'The patient reports less swelling and more confidence moving around the home. Rehabilitation milestones were discussed.', 'Initial ankle recovery discussion documented swelling after activity and limited walking tolerance.', 'Fictional pain-relief prescription review recorded; no new medication instructions were added.'),
    (3, 'wrist', 'Hand comfort and light daily tasks are improving. The therapist''s progress update was reviewed.', 'Initial wrist review documented stiffness and difficulty with grip-dependent activities.', 'Fictional discharge medication reconciliation completed; the recorded plan remains unchanged.'),
    (4, 'spine', 'Sitting tolerance and sleep have improved. Progress against the agreed rehabilitation goals was reviewed.', 'Initial spine recovery discussion documented reduced sitting tolerance and stiffness during daily activities.', 'Fictional medication reconciliation completed; a review with the care team is documented.'),
    (5, 'hip', 'The patient reports steadier movement and improved confidence with everyday activities. Therapy milestones were reviewed.', 'Initial hip review documented reduced mobility and a need for support with longer walks.', 'Fictional postoperative prescription review recorded; the existing clinician-approved plan is unchanged.')
)
SELECT i, first_name, last_name, first_name || ' ' || last_name AS name,
    lower(first_name || '.' || last_name) || '.' || lpad(i::text,3,'0') || '@example.com' AS email,
    make_date(2006-(i-1)%61, 1+(i-1)%8, 1+(i-1)%28) AS dob,
    (ARRAY['O+','A+','B+','AB+','O-','A-','B-','AB-'])[((i-1)%8)+1] AS blood_type,
    'DEMO-' || lpad(i::text,4,'0') AS visit,
    12+(i*7)%180 AS recent_hours,
    area, review, baseline, medication
FROM names JOIN scenarios ON n=(i-1)%6;

INSERT INTO public.profiles (role, name, email, dob, blood_type)
SELECT 'patient', name, email, dob, blood_type FROM pg_temp.clearchart_bulk_seed
ON CONFLICT (lower(email)) DO NOTHING;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_temp.clearchart_bulk_seed f
        LEFT JOIN public.profiles p ON lower(p.email) = f.email
        WHERE p.id IS NULL OR p.role <> 'patient'
    ) THEN
        RAISE EXCEPTION 'A bulk fixture email is missing or belongs to a non-patient profile; no seed changes were committed.';
    END IF;
END;
$$;

-- Resolve actual profile IDs only after insertion, supporting old fixed IDs too.
CREATE TEMP TABLE clearchart_bulk_patients ON COMMIT DROP AS
SELECT f.*, p.id AS patient_id, d.id AS doctor_id
FROM pg_temp.clearchart_bulk_seed f
JOIN public.profiles p ON lower(p.email) = f.email AND p.role = 'patient'
JOIN public.profiles d ON lower(d.email) = 'sarah.chen@example.com' AND d.role = 'doctor';

INSERT INTO public.care_team (patient_id, doctor_id)
SELECT patient_id, doctor_id FROM pg_temp.clearchart_bulk_patients
UNION ALL
SELECT b.patient_id, d.id
FROM pg_temp.clearchart_bulk_patients b
JOIN public.profiles d ON lower(d.email) = 'james.wilson@example.com' AND d.role = 'doctor'
WHERE b.i % 3 = 0
ON CONFLICT (patient_id, doctor_id) DO NOTHING;

WITH fixture AS (
    SELECT b.patient_id, b.doctor_id, r.kind AS type,
        b.name || ' | ' || b.visit || ' | ' || b.area || ' recovery. ' || r.content AS content,
        current_timestamp - ((b.recent_hours + r.extra_hours) * interval '1 hour') AS timestamp,
        CASE WHEN r.scan IS NOT NULL THEN '/mock-scans/' || b.patient_id::text || '/' || r.scan || '.svg' END AS image_url
    FROM pg_temp.clearchart_bulk_patients b
    CROSS JOIN LATERAL (VALUES
        ('note', 'Follow-up: ' || b.review || ' This is a fictional training record.', 0, NULL::text),
        ('imaging', 'Follow-up illustrative synthetic scan. Comparison image for this mock visit; no diagnostic interpretation.', 24, '2'),
        ('prescription', b.medication || ' Demo documentation only; no dosage or treatment instructions.', 48, NULL::text),
        ('note', 'Baseline: ' || b.baseline || ' Goals were recorded for the next fictional review.', 168, NULL::text),
        ('imaging', 'Baseline illustrative synthetic scan. Patient-specific demo reference image; no diagnostic interpretation.', 192, '1')
    ) AS r(kind, content, extra_hours, scan)
)
INSERT INTO public.medical_records (patient_id, doctor_id, type, content, timestamp, image_url)
SELECT f.patient_id, f.doctor_id, f.type, f.content, f.timestamp, f.image_url
FROM fixture f
WHERE NOT EXISTS (
    SELECT 1 FROM public.medical_records r
    WHERE r.patient_id = f.patient_id AND r.doctor_id = f.doctor_id
      AND r.type = f.type AND r.content = f.content
      AND r.image_url IS NOT DISTINCT FROM f.image_url
);

WITH fixture AS (
    SELECT patient_id,
        lower(first_name || '-' || last_name) || '-' || lpad(i::text,3,'0') || '-' || area || '-progress.pdf' AS file_name,
        current_timestamp - ((recent_hours + 6) * interval '1 hour') AS timestamp
    FROM pg_temp.clearchart_bulk_patients
)
INSERT INTO public.patient_uploads (patient_id, file_name, timestamp)
SELECT f.patient_id, f.file_name, f.timestamp
FROM fixture f
WHERE NOT EXISTS (
    SELECT 1 FROM public.patient_uploads u
    WHERE u.patient_id = f.patient_id AND u.file_name = f.file_name
);

WITH fixture AS (
    SELECT b.patient_id, h.kind AS status_type, h.value,
        current_timestamp - ((b.recent_hours + 1) * interval '1 hour') AS timestamp
    FROM pg_temp.clearchart_bulk_patients b
    CROSS JOIN LATERAL (VALUES ('pain', 1+b.i%6), ('mobility', 4+b.i%7), ('energy', 5+b.i%6)) AS h(kind, value)
)
INSERT INTO public.healing_progress (patient_id, status_type, value, timestamp)
SELECT f.patient_id, f.status_type, f.value, f.timestamp
FROM fixture f
WHERE NOT EXISTS (
    SELECT 1 FROM public.healing_progress h
    WHERE h.patient_id = f.patient_id AND h.status_type = f.status_type AND h.value = f.value
);

COMMIT;
