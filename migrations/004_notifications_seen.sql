-- Remember when a doctor last opened notifications so the unread badge resets.
-- Additive only: existing rows get NULL, so recent activity shows as unread once.
BEGIN;
ALTER TABLE public.profiles ADD COLUMN IF NOT EXISTS notifications_seen_at timestamptz;
COMMENT ON COLUMN public.profiles.notifications_seen_at IS 'When this doctor last opened the notification panel; newer invitation responses count as unread.';
COMMIT;
