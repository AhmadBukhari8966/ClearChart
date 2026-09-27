-- ClearChart hackathon schema. Run this first in the Supabase SQL editor,
-- then run seed.sql. Tables contain synthetic demonstration data only.
-- The Go server uses a privileged PostgreSQL connection (DATABASE_URL).
-- RLS is enabled with no browser policies: anonymous/authenticated Supabase
-- clients cannot read or mutate these tables. Do not expose DATABASE_URL.
-- Authentication uses Supabase Auth. Seeded profiles remain unclaimed until
-- an administrator explicitly associates an auth_user_id.

BEGIN;

CREATE TABLE IF NOT EXISTS public.profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    auth_user_id uuid,
    role text NOT NULL CHECK (role IN ('doctor', 'patient')),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 2 AND 120),
    email text NOT NULL CHECK (length(email) BETWEEN 3 AND 254),
    license_num text,
    specialization text,
    dob date,
    blood_type text CHECK (blood_type IN ('A+', 'A-', 'B+', 'B-', 'AB+', 'AB-', 'O+', 'O-', 'Unknown')),
    CONSTRAINT profiles_onboarding_fields CHECK (
        (role = 'doctor' AND length(btrim(license_num)) > 0
            AND license_num IS NOT NULL AND length(btrim(specialization)) > 0
            AND specialization IS NOT NULL)
        OR (role = 'patient' AND dob IS NOT NULL AND blood_type IS NOT NULL)
    )
);
-- Also upgrade existing installations without changing their seeded data.
ALTER TABLE public.profiles ADD COLUMN IF NOT EXISTS auth_user_id uuid;
CREATE UNIQUE INDEX IF NOT EXISTS profiles_auth_user_id_unique ON public.profiles (auth_user_id);
COMMENT ON COLUMN public.profiles.auth_user_id IS 'Verified Supabase Auth user ID. Null for unclaimed seeded profiles. Assigned by the server after authentication.';
CREATE UNIQUE INDEX IF NOT EXISTS profiles_email_unique ON public.profiles (lower(email));
CREATE INDEX IF NOT EXISTS profiles_role_idx ON public.profiles (role);

CREATE TABLE IF NOT EXISTS public.care_team (
    patient_id uuid NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
    doctor_id uuid NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
    PRIMARY KEY (patient_id, doctor_id),
    CHECK (patient_id <> doctor_id)
);
CREATE INDEX IF NOT EXISTS care_team_doctor_idx ON public.care_team (doctor_id);

CREATE TABLE IF NOT EXISTS public.medical_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    patient_id uuid NOT NULL REFERENCES public.profiles(id),
    doctor_id uuid NOT NULL REFERENCES public.profiles(id),
    type text NOT NULL CHECK (type IN ('note', 'prescription', 'imaging')),
    content text NOT NULL CHECK (length(btrim(content)) BETWEEN 1 AND 10000),
    timestamp timestamptz NOT NULL DEFAULT current_timestamp,
    image_url text,
    -- Only linked clinicians may author a patient's records. Relationships
    -- with existing records cannot be removed in this small prototype.
    FOREIGN KEY (patient_id, doctor_id) REFERENCES public.care_team(patient_id, doctor_id)
);
CREATE INDEX IF NOT EXISTS medical_records_patient_time_idx ON public.medical_records (patient_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS medical_records_doctor_idx ON public.medical_records (doctor_id);

CREATE TABLE IF NOT EXISTS public.patient_uploads (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    patient_id uuid NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
    file_name text NOT NULL CHECK (length(btrim(file_name)) BETWEEN 1 AND 255),
    timestamp timestamptz NOT NULL DEFAULT current_timestamp
);
COMMENT ON TABLE public.patient_uploads IS 'Filename metadata only. The demo never persists uploaded file bytes.';
CREATE INDEX IF NOT EXISTS patient_uploads_patient_time_idx ON public.patient_uploads (patient_id, timestamp DESC);

CREATE TABLE IF NOT EXISTS public.healing_progress (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    patient_id uuid NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
    status_type text NOT NULL CHECK (status_type IN ('pain', 'mobility', 'energy')),
    value integer NOT NULL CHECK (value BETWEEN 1 AND 10),
    timestamp timestamptz NOT NULL DEFAULT current_timestamp
);
COMMENT ON COLUMN public.healing_progress.value IS 'Patient reported 1–10 score. Lower pain is better; higher mobility and energy are better.';
CREATE INDEX IF NOT EXISTS healing_progress_latest_idx ON public.healing_progress (patient_id, status_type, timestamp DESC);

CREATE TABLE IF NOT EXISTS public.mock_biometric_data (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    doctor_id uuid NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
    heart_rate integer NOT NULL CHECK (heart_rate BETWEEN 31 AND 220),
    sleep_hours numeric(4,2) NOT NULL CHECK (sleep_hours BETWEEN 0 AND 24),
    timestamp timestamptz NOT NULL DEFAULT current_timestamp
);
COMMENT ON TABLE public.mock_biometric_data IS 'Synthetic Apple Watch presentation data. No device connection or HealthKit integration.';
CREATE INDEX IF NOT EXISTS mock_biometric_data_doctor_time_idx ON public.mock_biometric_data (doctor_id, timestamp DESC);

-- Foreign keys ensure existence; these guards also ensure the referenced
-- profile has the correct role. An immutable role keeps existing links valid.
CREATE OR REPLACE FUNCTION public.clearchart_check_profile_role()
RETURNS trigger LANGUAGE plpgsql SET search_path = public, pg_temp AS $$
DECLARE referenced_id uuid;
BEGIN
    referenced_id := (to_jsonb(NEW) ->> TG_ARGV[0])::uuid;
    IF NOT EXISTS (SELECT 1 FROM public.profiles WHERE id = referenced_id AND role = TG_ARGV[1]) THEN
        RAISE EXCEPTION 'Invalid profile role for %', TG_ARGV[0] USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION public.clearchart_keep_profile_role()
RETURNS trigger LANGUAGE plpgsql SET search_path = public, pg_temp AS $$
BEGIN
    IF NEW.role IS DISTINCT FROM OLD.role THEN
        RAISE EXCEPTION 'Profile roles cannot be changed' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'clearchart_profile_role_immutable' AND tgrelid = 'public.profiles'::regclass) THEN
        CREATE TRIGGER clearchart_profile_role_immutable BEFORE UPDATE OF role ON public.profiles FOR EACH ROW EXECUTE FUNCTION public.clearchart_keep_profile_role();
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'clearchart_care_patient_role' AND tgrelid = 'public.care_team'::regclass) THEN
        CREATE TRIGGER clearchart_care_patient_role BEFORE INSERT OR UPDATE ON public.care_team FOR EACH ROW EXECUTE FUNCTION public.clearchart_check_profile_role('patient_id', 'patient');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'clearchart_care_doctor_role' AND tgrelid = 'public.care_team'::regclass) THEN
        CREATE TRIGGER clearchart_care_doctor_role BEFORE INSERT OR UPDATE ON public.care_team FOR EACH ROW EXECUTE FUNCTION public.clearchart_check_profile_role('doctor_id', 'doctor');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'clearchart_upload_patient_role' AND tgrelid = 'public.patient_uploads'::regclass) THEN
        CREATE TRIGGER clearchart_upload_patient_role BEFORE INSERT OR UPDATE ON public.patient_uploads FOR EACH ROW EXECUTE FUNCTION public.clearchart_check_profile_role('patient_id', 'patient');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'clearchart_healing_patient_role' AND tgrelid = 'public.healing_progress'::regclass) THEN
        CREATE TRIGGER clearchart_healing_patient_role BEFORE INSERT OR UPDATE ON public.healing_progress FOR EACH ROW EXECUTE FUNCTION public.clearchart_check_profile_role('patient_id', 'patient');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'clearchart_biometric_doctor_role' AND tgrelid = 'public.mock_biometric_data'::regclass) THEN
        CREATE TRIGGER clearchart_biometric_doctor_role BEFORE INSERT OR UPDATE ON public.mock_biometric_data FOR EACH ROW EXECUTE FUNCTION public.clearchart_check_profile_role('doctor_id', 'doctor');
    END IF;
END;
$$;

ALTER TABLE public.profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.care_team ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.medical_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.patient_uploads ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.healing_progress ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.mock_biometric_data ENABLE ROW LEVEL SECURITY;

COMMIT;

-- Patient consent invitations (also available as migration 002).
BEGIN;
CREATE TABLE IF NOT EXISTS public.care_invitations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 doctor_id uuid NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
 email text NOT NULL CHECK (length(email) BETWEEN 3 AND 254),
 token_hash text NOT NULL UNIQUE CHECK (length(token_hash)=64),
 status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','declined','revoked')),
 patient_id uuid REFERENCES public.profiles(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL DEFAULT (now()+interval '7 days'),
 responded_at timestamptz
);
CREATE INDEX IF NOT EXISTS care_invitations_doctor_created ON public.care_invitations(doctor_id,created_at DESC);
ALTER TABLE public.care_invitations ENABLE ROW LEVEL SECURITY;
COMMIT;

-- Explicit record body-area categories (also available as migration 003).
-- Additive only: existing records stay untouched and remain uncategorized.
BEGIN;
CREATE TABLE IF NOT EXISTS public.medical_record_categories (
 record_id uuid NOT NULL REFERENCES public.medical_records(id) ON DELETE CASCADE,
 category text NOT NULL CHECK (category IN ('brain','heart','lungs','liver','stomach','kidneys','spine','shoulders','knees','ankles','hips','blood','nervous-system','muscles')),
 PRIMARY KEY (record_id, category)
);
CREATE INDEX IF NOT EXISTS medical_record_categories_category_idx ON public.medical_record_categories(category, record_id);
ALTER TABLE public.medical_record_categories ENABLE ROW LEVEL SECURITY;
COMMIT;
