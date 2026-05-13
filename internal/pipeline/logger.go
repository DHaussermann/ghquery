package pipeline

import (
	"fmt"
	"io"
	"sync"
)

// Logger is a thread-safe writer for pipeline log output.
// Both CLI (os.Stderr) and web (SSE stream) use this as their log destination.
type Logger struct {
	w  io.Writer
	mu sync.Mutex
}

func NewLogger(w io.Writer) *Logger {
	return &Logger{w: w}
}

func (l *Logger) Logf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, format, args...)
}

// Writer returns the underlying io.Writer for passing to sub-packages.
func (l *Logger) Writer() io.Writer {
	return &syncWriter{w: l.w, mu: &l.mu}
}

// syncWriter wraps an io.Writer with a shared mutex so concurrent goroutines
// (e.g. the 3 analysis agents) don't interleave their output.
type syncWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (sw *syncWriter) Write(p []byte) (int, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.w.Write(p)
}
