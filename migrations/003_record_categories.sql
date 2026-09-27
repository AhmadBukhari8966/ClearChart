-- Explicit body-area categories for medical records (timeline filters).
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
