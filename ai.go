package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// explainSystemPrompt tells the model to translate, not diagnose.
const explainSystemPrompt = `You help patients understand notes their doctor wrote about them. Rewrite the doctor's note in plain, warm, everyday language that someone with no medical training can follow (about a 6th-grade reading level).

Rules:
- Explain what the doctor meant. Replace medical terms and abbreviations with simple words (for example "edema" becomes "swelling", "BID" becomes "twice a day", "ROM" becomes "how far you can move the joint").
- Stay faithful to the note. Do not add diagnoses, test results, medications, doses, predictions or advice that the note does not contain. If something in the note is unclear, say it is a good question to ask the doctor instead of guessing.
- Keep any instructions from the note (what to do, how often, what to avoid, when to come back) and make them easy to spot.
- Speak directly to the patient as "you". Be calm and reassuring without downplaying anything the doctor flagged.
- Reply in plain text only: no markdown, no asterisks, no headings. Use 2 to 5 short sentences. If there are several instructions, put each on its own line starting with "- ".
- If the note mentions urgent warning signs, keep them and make clear they matter.`

const failureCooldown = 2 * time.Minute

const geminiEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"

type explainer struct {
	key, model string
	client     *http.Client
	mu         sync.Mutex
	cache      map[[32]byte]string    // note hash -> explanation; notes are immutable so entries never go stale
	pending    map[[32]byte]bool      // generations running in the background
	failed     map[[32]byte]time.Time // last failure; retried after failureCooldown
}

// newExplainer returns nil when GEMINI_API_KEY is unset; callers then use the rule-based summary.
func newExplainer() *explainer {
	key := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if key == "" {
		return nil
	}
	model := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
	if model == "" {
		model = "gemini-3.5-flash-lite"
	}
	return &explainer{key: key, model: model, client: &http.Client{Timeout: 25 * time.Second}, cache: map[[32]byte]string{}, pending: map[[32]byte]bool{}, failed: map[[32]byte]time.Time{}}
}

func notePrompt(r Record) string {
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "Note written by %s on %s", strings.TrimSpace(r.DoctorName), r.Timestamp.Format("January 2, 2006"))
	if len(r.Categories) > 0 {
		names := make([]string, len(r.Categories))
		for i, c := range r.Categories {
			names[i] = categoryName(c)
		}
		fmt.Fprintf(&prompt, " (body areas: %s)", strings.Join(names, ", "))
	}
	fmt.Fprintf(&prompt, ":\n\n%s", r.Content)
	return prompt.String()
}

// lookup returns a cached explanation. On a miss it starts one background
// generation per note (never tied to a page request, so reloads and SSE
// reconnects cannot cancel it) and calls done when it finishes either way.
// After a failure the caller gets ok=false, pending=false until the cooldown ends.
func (e *explainer) lookup(r Record, done func()) (text string, ok, pending bool) {
	prompt := notePrompt(r)
	hash := sha256.Sum256([]byte(e.model + "|" + prompt))
	e.mu.Lock()
	defer e.mu.Unlock()
	if text, ok = e.cache[hash]; ok {
		return text, true, false
	}
	if at, failed := e.failed[hash]; failed && time.Since(at) < failureCooldown {
		return "", false, false
	}
	if !e.pending[hash] {
		e.pending[hash] = true
		go e.generate(hash, prompt, done)
	}
	return "", false, true
}

func (e *explainer) generate(hash [32]byte, prompt string, done func()) {
	text, err := e.call(prompt)
	if err != nil && isBusy(err) {
		time.Sleep(3 * time.Second) // Gemini often answers 503 "high demand" briefly
		text, err = e.call(prompt)
	}
	e.mu.Lock()
	delete(e.pending, hash)
	if err == nil {
		if len(e.cache) >= 1000 {
			clear(e.cache)
		}
		e.cache[hash] = text
		delete(e.failed, hash)
	} else {
		if len(e.failed) >= 1000 {
			clear(e.failed)
		}
		e.failed[hash] = time.Now()
	}
	e.mu.Unlock()
	if err != nil {
		log.Printf("AI summary unavailable, using rule-based text: %v", err)
	}
	done() // re-render the card: explanation, or rule-based text instead of the loading state
}

type statusError struct {
	code int
	msg  string
}

func (e statusError) Error() string { return fmt.Sprintf("gemini: status %d: %s", e.code, e.msg) }

func isBusy(err error) bool {
	var s statusError
	return errors.As(err, &s) && (s.code == http.StatusServiceUnavailable || s.code == http.StatusTooManyRequests)
}

func (e *explainer) call(prompt string) (string, error) {
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	gen := map[string]any{"temperature": 0.3}
	if strings.HasPrefix(e.model, "gemini-2.5-flash") {
		gen["thinkingConfig"] = map[string]int{"thinkingBudget": 0} // this task needs no reasoning
	}
	body, err := json.Marshal(map[string]any{
		"system_instruction": content{Parts: []part{{explainSystemPrompt}}},
		"contents":           []content{{Role: "user", Parts: []part{{prompt}}}},
		"generationConfig":   gen,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf(geminiEndpoint, e.model), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", e.key)
	resp, err := e.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Candidates []struct {
			Content content `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", statusError{resp.StatusCode, "unreadable response"}
	}
	if resp.StatusCode != http.StatusOK {
		msg := "unknown error"
		if out.Error != nil {
			msg = out.Error.Message
		}
		return "", statusError{resp.StatusCode, msg}
	}
	var result strings.Builder
	if len(out.Candidates) > 0 {
		for _, p := range out.Candidates[0].Content.Parts {
			result.WriteString(p.Text)
		}
	}
	explanation := strings.TrimSpace(strings.ReplaceAll(result.String(), "**", ""))
	if explanation == "" {
		return "", errors.New("gemini: empty response")
	}
	return explanation, nil
}

// patientSummary explains the latest doctor note with Gemini. Until the
// explanation is ready (or if Gemini fails) it returns the rule-based text;
// when a background generation finishes, the patient's live stream re-renders
// the card from the cache.
func (a *app) patientSummary(patientID string, records []Record) (text string, ai, pending bool) {
	if a.explainer != nil {
		for _, r := range records {
			if r.Type != "note" {
				continue
			}
			if text, ok, pending := a.explainer.lookup(r, func() { a.hub.publish(patientID, "") }); ok {
				return text, true, false
			} else if pending {
				return plainSummary(records), false, true
			}
			break
		}
	}
	return plainSummary(records), false, false
}
