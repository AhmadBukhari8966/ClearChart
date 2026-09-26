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
