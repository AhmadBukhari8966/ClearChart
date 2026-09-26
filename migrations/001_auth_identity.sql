-- Run once in the Supabase SQL editor for an existing ClearChart database.
-- Safe to rerun: preserves every profile ID, care-team link, and medical record.
-- Existing synthetic profiles remain unclaimed (auth_user_id IS NULL).
BEGIN;
ALTER TABLE public.profiles ADD COLUMN IF NOT EXISTS auth_user_id uuid;
CREATE UNIQUE INDEX IF NOT EXISTS profiles_auth_user_id_unique ON public.profiles (auth_user_id);
COMMENT ON COLUMN public.profiles.auth_user_id IS 'Verified Supabase Auth user ID. Null for unclaimed seeded profiles. Assigned by the server after authentication.';
COMMIT;
