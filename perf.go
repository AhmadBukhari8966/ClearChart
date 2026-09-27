package main

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
)

// parallel runs independent loads concurrently, so a page waits for the
// slowest round trip instead of their sum. It returns the first error.
func parallel(fns ...func() error) error {
	errs := make([]error, len(fns))
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = fn() }()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// staticETags fingerprints embedded assets once at startup. Embedded files have
// no modification time, so without an ETag browsers must refetch them in full.
func staticETags(static fs.FS) map[string]string {
	tags := map[string]string{}
	fs.WalkDir(static, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, err := fs.ReadFile(static, p); err == nil {
			sum := sha256.Sum256(b)
			tags["/"+p] = `"` + hex.EncodeToString(sum[:8]) + `"`
		}
		return nil
	})
	return tags
}

// Compressible text responses. SSE streams are excluded: they are small,
// long-lived and flushed per batch, and their writers need deadline control.
var gzipTypes = []string{"text/html", "text/css", "image/svg+xml", "text/javascript", "application/javascript", "text/plain"}

var gzipPool = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed); return w }}

type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

func (g *gzipWriter) decide() {
	if g.decided {
		return
	}
	g.decided = true
	h := g.Header()
	ct := h.Get("Content-Type")
	if h.Get("Content-Encoding") != "" || strings.HasPrefix(ct, "text/event-stream") {
		return
	}
	for _, t := range gzipTypes {
		if strings.HasPrefix(ct, t) {
			h.Del("Content-Length")
			h.Set("Content-Encoding", "gzip")
			g.gz = gzipPool.Get().(*gzip.Writer)
			g.gz.Reset(g.ResponseWriter)
			return
		}
	}
}

func (g *gzipWriter) WriteHeader(code int) {
	if code != http.StatusNotModified && code != http.StatusNoContent {
		g.decide()
	} else {
		g.decided = true
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.decided {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.decide()
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipWriter) Flush() {
	if g.gz != nil {
		g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (g *gzipWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipWriter) close() {
	if g.gz != nil {
		g.gz.Close()
		g.gz.Reset(io.Discard)
		gzipPool.Put(g.gz)
	}
}

func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.Method == http.MethodHead || r.Header.Get("Range") != "" || strings.HasPrefix(r.URL.Path, "/events/") {
			next.ServeHTTP(w, r)
			return
		}
		g := &gzipWriter{ResponseWriter: w}
		defer g.close()
		next.ServeHTTP(g, r)
	})
}
