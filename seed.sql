-- Run after schema.sql. Deterministic IDs and ON CONFLICT make reruns safe.
-- All people, records, reports and measurements below are fictional demo data.
BEGIN;

INSERT INTO public.profiles (id, role, name, email, license_num, specialization, dob, blood_type) VALUES
('00000000-0000-4000-8000-000000000001', 'doctor', 'Dr. Sarah Chen', 'sarah.chen@example.com', 'DEMO-ORTHO-2048', 'Orthopedics', NULL, NULL),
('00000000-0000-4000-8000-000000000002', 'doctor', 'Dr. James Wilson', 'james.wilson@example.com', 'DEMO-PCP-1024', 'Primary care', NULL, NULL),
('00000000-0000-4000-8000-000000000101', 'patient', 'Alex Morgan', 'alex.morgan@example.com', NULL, NULL, '1994-06-15', 'O+'),
('00000000-0000-4000-8000-000000000102', 'patient', 'Jordan Lee', 'jordan.lee@example.com', NULL, NULL, '1987-03-22', 'A+'),
('00000000-0000-4000-8000-000000000103', 'patient', 'Taylor Brooks', 'taylor.brooks@example.com', NULL, NULL, '2000-11-08', 'B+')
ON CONFLICT (id) DO NOTHING;

INSERT INTO public.care_team (patient_id, doctor_id) VALUES
('00000000-0000-4000-8000-000000000101', '00000000-0000-4000-8000-000000000001'),
('00000000-0000-4000-8000-000000000101', '00000000-0000-4000-8000-000000000002'),
('00000000-0000-4000-8000-000000000102', '00000000-0000-4000-8000-000000000001'),
('00000000-0000-4000-8000-000000000103', '00000000-0000-4000-8000-000000000002')
ON CONFLICT DO NOTHING;

INSERT INTO public.medical_records (id, patient_id, doctor_id, type, content, timestamp, image_url) VALUES
('10000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', '00000000-0000-4000-8000-000000000001', 'note', 'Two-week knee recovery review: incision is healing well. Swelling has reduced and range of motion is improving. Continue the rehabilitation plan and follow up in two weeks.', current_timestamp - interval '2 hours', NULL),
('10000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000101', '00000000-0000-4000-8000-000000000001', 'prescription', 'Demo medication plan: acetaminophen as directed on the discharge instructions, only when needed. Review all medications with your care team.', current_timestamp - interval '26 hours', NULL),
('10000000-0000-4000-8000-000000000003', '00000000-0000-4000-8000-000000000101', '00000000-0000-4000-8000-000000000001', 'imaging', 'Follow-up knee imaging: postoperative alignment is maintained. No new concerning findings in this simulated study.', current_timestamp - interval '50 hours', '/static/ct-scan.svg'),
('10000000-0000-4000-8000-000000000004', '00000000-0000-4000-8000-000000000101', '00000000-0000-4000-8000-000000000002', 'note', 'Recovery check-in: sleep is improving and Alex is walking more comfortably with support. Keep sharing any changes with the care team.', current_timestamp - interval '74 hours', NULL),
('10000000-0000-4000-8000-000000000005', '00000000-0000-4000-8000-000000000101', '00000000-0000-4000-8000-000000000001', 'note', 'Initial postoperative review: begin the agreed gentle movement plan with your physiotherapist. Expected swelling is present; the wound looks clean.', current_timestamp - interval '170 hours', NULL),
('10000000-0000-4000-8000-000000000006', '00000000-0000-4000-8000-000000000102', '00000000-0000-4000-8000-000000000001', 'note', 'Shoulder follow-up: mobility is improving with physiotherapy. Continue the agreed exercise plan and review next month.', current_timestamp - interval '5 hours', NULL),
('10000000-0000-4000-8000-000000000007', '00000000-0000-4000-8000-000000000102', '00000000-0000-4000-8000-000000000001', 'prescription', 'Demo prescription review: continue current care plan. Medication questions will be reviewed at the next appointment.', current_timestamp - interval '52 hours', NULL),
('10000000-0000-4000-8000-000000000008', '00000000-0000-4000-8000-000000000102', '00000000-0000-4000-8000-000000000001', 'imaging', 'Simulated shoulder imaging reviewed. Findings are consistent with the established recovery plan.', current_timestamp - interval '100 hours', '/static/ct-scan.svg'),
('10000000-0000-4000-8000-000000000009', '00000000-0000-4000-8000-000000000103', '00000000-0000-4000-8000-000000000002', 'note', 'Ankle recovery check: less swelling reported and daily activity is gradually increasing. Follow up as scheduled.', current_timestamp - interval '8 hours', NULL),
('10000000-0000-4000-8000-000000000010', '00000000-0000-4000-8000-000000000103', '00000000-0000-4000-8000-000000000002', 'prescription', 'Demo medication reconciliation completed. No changes to the discharge medication plan.', current_timestamp - interval '76 hours', NULL),
('10000000-0000-4000-8000-000000000011', '00000000-0000-4000-8000-000000000103', '00000000-0000-4000-8000-000000000002', 'imaging', 'Simulated ankle imaging reviewed with the patient. Recovery remains on the expected course.', current_timestamp - interval '124 hours', '/static/ct-scan.svg')
ON CONFLICT (id) DO NOTHING;

INSERT INTO public.patient_uploads (id, patient_id, file_name, timestamp) VALUES
('20000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', 'physiotherapy-progress.pdf', current_timestamp - interval '4 hours'),
('20000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000101', 'discharge-summary.pdf', current_timestamp - interval '168 hours'),
('20000000-0000-4000-8000-000000000003', '00000000-0000-4000-8000-000000000102', 'shoulder-exercises.pdf', current_timestamp - interval '12 hours'),
('20000000-0000-4000-8000-000000000004', '00000000-0000-4000-8000-000000000103', 'ankle-recovery-notes.pdf', current_timestamp - interval '20 hours')
ON CONFLICT (id) DO NOTHING;

INSERT INTO public.healing_progress (id, patient_id, status_type, value, timestamp) VALUES
('30000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', 'pain', 6, current_timestamp - interval '168 hours'),
('30000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000101', 'mobility', 3, current_timestamp - interval '168 hours'),
('30000000-0000-4000-8000-000000000003', '00000000-0000-4000-8000-000000000101', 'energy', 4, current_timestamp - interval '168 hours'),
('30000000-0000-4000-8000-000000000004', '00000000-0000-4000-8000-000000000101', 'pain', 3, current_timestamp - interval '3 hours'),
('30000000-0000-4000-8000-000000000005', '00000000-0000-4000-8000-000000000101', 'mobility', 7, current_timestamp - interval '3 hours'),
('30000000-0000-4000-8000-000000000006', '00000000-0000-4000-8000-000000000101', 'energy', 8, current_timestamp - interval '3 hours'),
('30000000-0000-4000-8000-000000000007', '00000000-0000-4000-8000-000000000102', 'pain', 2, current_timestamp - interval '6 hours'),
('30000000-0000-4000-8000-000000000008', '00000000-0000-4000-8000-000000000102', 'mobility', 8, current_timestamp - interval '6 hours'),
('30000000-0000-4000-8000-000000000009', '00000000-0000-4000-8000-000000000102', 'energy', 7, current_timestamp - interval '6 hours'),
('30000000-0000-4000-8000-000000000010', '00000000-0000-4000-8000-000000000103', 'pain', 4, current_timestamp - interval '9 hours'),
('30000000-0000-4000-8000-000000000011', '00000000-0000-4000-8000-000000000103', 'mobility', 6, current_timestamp - interval '9 hours'),
('30000000-0000-4000-8000-000000000012', '00000000-0000-4000-8000-000000000103', 'energy', 7, current_timestamp - interval '9 hours')
ON CONFLICT (id) DO NOTHING;

INSERT INTO public.mock_biometric_data (id, doctor_id, heart_rate, sleep_hours, timestamp) VALUES
('40000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', 64, 7.60, current_timestamp - interval '30 minutes'),
('40000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000002', 68, 7.20, current_timestamp - interval '45 minutes')
ON CONFLICT (id) DO NOTHING;

COMMIT;
