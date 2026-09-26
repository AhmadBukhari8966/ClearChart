-- Optional large ClearChart fixture: run AFTER schema.sql and seed.sql.
-- Adds 300 fictional patients, 1,500 records (including 600 image references),
-- 300 filename-only uploads, 900 healing scores and 400 care-team links.
-- Deterministic IDs and ON CONFLICT preserve existing data on repeat runs.
-- Matches the automatic in-memory fixture in bulk_seed.go.
BEGIN;

DO $$
BEGIN
    IF (SELECT count(*) FROM public.profiles
        WHERE id IN ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002')
        AND role = 'doctor') <> 2 THEN
        RAISE EXCEPTION 'Run schema.sql and seed.sql before seed_large.sql: both demo doctors are required.';
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
    ('40000000-0000-4000-8000-' || lpad(i::text,12,'0'))::uuid AS patient_id,
    lower(first_name || '.' || last_name) || '.' || lpad(i::text,3,'0') || '@example.com' AS email,
    make_date(2006-(i-1)%61, 1+(i-1)%8, 1+(i-1)%28) AS dob,
    (ARRAY['O+','A+','B+','AB+','O-','A-','B-','AB-'])[((i-1)%8)+1] AS blood_type,
    'DEMO-' || lpad(i::text,4,'0') AS visit,
    12+(i*7)%180 AS recent_hours,
    area, review, baseline, medication
FROM names JOIN scenarios ON n=(i-1)%6;

INSERT INTO public.profiles (id,role,name,email,dob,blood_type)
SELECT patient_id,'patient',name,email,dob,blood_type FROM clearchart_bulk_seed
ON CONFLICT (id) DO NOTHING;

INSERT INTO public.care_team (patient_id,doctor_id)
SELECT patient_id,'00000000-0000-4000-8000-000000000001'::uuid FROM clearchart_bulk_seed
UNION ALL
SELECT patient_id,'00000000-0000-4000-8000-000000000002'::uuid FROM clearchart_bulk_seed WHERE i%3=0
ON CONFLICT DO NOTHING;

INSERT INTO public.medical_records (id,patient_id,doctor_id,type,content,timestamp,image_url)
SELECT ('50000000-0000-4000-8000-' || lpad(((i-1)*5+r.ordinal)::text,12,'0'))::uuid,
    patient_id, '00000000-0000-4000-8000-000000000001'::uuid, r.kind,
    name || ' | ' || visit || ' | ' || area || ' recovery. ' || r.content,
    current_timestamp - ((recent_hours+r.extra_hours) * interval '1 hour'),
    CASE WHEN r.scan IS NOT NULL THEN '/mock-scans/' || patient_id::text || '/' || r.scan || '.svg' END
FROM clearchart_bulk_seed
CROSS JOIN LATERAL (VALUES
    (1, 'note', 'Follow-up: ' || review || ' This is a fictional training record.', 0, NULL::text),
    (2, 'imaging', 'Follow-up illustrative synthetic scan. Comparison image for this mock visit; no diagnostic interpretation.', 24, '2'),
    (3, 'prescription', medication || ' Demo documentation only; no dosage or treatment instructions.', 48, NULL::text),
    (4, 'note', 'Baseline: ' || baseline || ' Goals were recorded for the next fictional review.', 168, NULL::text),
    (5, 'imaging', 'Baseline illustrative synthetic scan. Patient-specific demo reference image; no diagnostic interpretation.', 192, '1')
) AS r(ordinal,kind,content,extra_hours,scan)
ON CONFLICT (id) DO NOTHING;

INSERT INTO public.patient_uploads (id,patient_id,file_name,timestamp)
SELECT ('60000000-0000-4000-8000-' || lpad(i::text,12,'0'))::uuid, patient_id,
    lower(first_name || '-' || last_name) || '-' || lpad(i::text,3,'0') || '-' || area || '-progress.pdf',
    current_timestamp - ((recent_hours+6) * interval '1 hour')
FROM clearchart_bulk_seed
ON CONFLICT (id) DO NOTHING;

INSERT INTO public.healing_progress (id,patient_id,status_type,value,timestamp)
SELECT ('70000000-0000-4000-8000-' || lpad(((i-1)*3+h.ordinal)::text,12,'0'))::uuid,
    patient_id, h.kind, h.value,
    current_timestamp - ((recent_hours+1) * interval '1 hour')
FROM clearchart_bulk_seed
CROSS JOIN LATERAL (VALUES (1,'pain',1+i%6), (2,'mobility',4+i%7), (3,'energy',5+i%6)) AS h(ordinal,kind,value)
ON CONFLICT (id) DO NOTHING;

COMMIT;
