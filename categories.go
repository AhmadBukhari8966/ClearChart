package main

// RecordCategory is a body area a clinician explicitly tags on a record.
// The IDs match the former body-map areas and the database CHECK constraint
// in migrations/003_record_categories.sql. Records are never auto-tagged.
type RecordCategory struct{ ID, Name string }

var recordCategories = []RecordCategory{
	{"brain", "Brain"},
	{"heart", "Heart"},
	{"lungs", "Lungs"},
	{"liver", "Liver"},
	{"stomach", "Stomach"},
	{"kidneys", "Kidneys"},
	{"spine", "Spine"},
	{"shoulders", "Shoulders"},
	{"knees", "Knees"},
	{"ankles", "Ankles"},
	{"hips", "Hips"},
	{"blood", "Blood"},
	{"nervous-system", "Nervous system"},
	{"muscles", "Muscles"},
}

func validCategory(id string) bool { return categoryName(id) != "" }

func categoryName(id string) string {
	for _, c := range recordCategories {
		if c.ID == id {
			return c.Name
		}
	}
	return ""
}

// timelineFilter combines the record-type row and the category row.
// Empty values mean "All types" and "All categories".
type timelineFilter struct{ Type, Category string }

func (f timelineFilter) valid() bool {
	return (f.Type == "" || validType(f.Type)) && (f.Category == "" || validCategory(f.Category))
}

func (f timelineFilter) matches(r Record) bool {
	if f.Type != "" && r.Type != f.Type {
		return false
	}
	if f.Category == "" {
		return true
	}
	for _, c := range r.Categories {
		if c == f.Category {
			return true
		}
	}
	return false
}

// normalizeCategories keeps known, unique categories in canonical order.
func normalizeCategories(values []string) ([]string, bool) {
	seen := map[string]bool{}
	for _, v := range values {
		if !validCategory(v) {
			return nil, false
		}
		seen[v] = true
	}
	var out []string
	for _, c := range recordCategories {
		if seen[c.ID] {
			out = append(out, c.ID)
		}
	}
	return out, true
}
