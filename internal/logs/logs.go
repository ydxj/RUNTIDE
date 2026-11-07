// Package logs is the central log hub: bounded in-memory ring buffers per
// service, live subscriptions, redaction, and batched SQLite persistence.
package logs

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/runtide/runtide/internal/redact"
)

// MaxLineBytes caps a single stored line.
const MaxLineBytes = 16 * 1024

// Entry is one log line. These are *application* logs captured from processes
// RUNTIDE launched; they are unrelated to the Windows Event Log.
type Entry struct {
	ID      uint64    `json:"id"`
	Service string    `json:"service"`
	Time    time.Time `json:"time"`
	Stream  string    `json:"stream"` // stdout | stderr | system
	Level   string    `json:"level"`  // error | warn | info | debug | ""
	Line    string    `json:"line"`
}

var (
	errRe   = regexp.MustCompile(`(?i)\b(error|err|fatal|panic|exception|failed|failure|critical|traceback)\b|\[ERROR\]|ERR!`)
	warnRe  = regexp.MustCompile(`(?i)\b(warn|warning|deprecated)\b|\[WARN\]|WARN!`)
	debugRe = regexp.MustCompile(`(?i)\b(debug|trace|verbose)\b`)
)

// DetectLevel guesses a severity from the text.
func DetectLevel(line string) string {
	switch {
	case errRe.MatchString(line):
		return "error"
	case warnRe.MatchString(line):
		return "warn"
	case debugRe.MatchString(line):
		return "debug"
	}
	return ""
}

type ring struct {
	buf   []Entry
	start int
	n     int
}

func (r *ring) push(e Entry) {
	if r.n < len(r.buf) {
		r.buf[(r.start+r.n)%len(r.buf)] = e
		r.n++
		return
	}
	r.buf[r.start] = e
	r.start = (r.start + 1) % len(r.buf)
}

func (r *ring) snapshot() []Entry {
	out := make([]Entry, r.n)
	for i := 0; i < r.n; i++ {
		out[i] = r.buf[(r.start+i)%len(r.buf)]
	}
	return out
}

// Hub collects and distributes log entries.
type Hub struct {
	mu       sync.RWMutex
	rings    map[string]*ring
	capacity int
	subs     map[int]*sub
	subSeq   int
	nextID   atomic.Uint64
	red      *redact.Redactor
	persist  chan Entry
	dropped  atomic.Uint64
}

type sub struct {
	ch       chan Entry
	services map[string]bool // empty = all
}

// NewHub creates a hub keeping capacity lines per service in memory.
func NewHub(capacity int, red *redact.Redactor) *Hub {
	if capacity < 100 {
		capacity = 100
	}
	if red == nil {
		red = redact.New()
	}
	return &Hub{rings: map[string]*ring{}, capacity: capacity, subs: map[int]*sub{}, red: red}
}

// SetRedactor swaps the redaction rules at runtime.
func (h *Hub) SetRedactor(r *redact.Redactor) {
	h.mu.Lock()
	h.red = r
	h.mu.Unlock()
}

// Append redacts, truncates, stores and broadcasts a line.
func (h *Hub) Append(service, stream, line string) Entry {
	line = strings.TrimRight(line, "\r\n")
	if len(line) > MaxLineBytes {
		line = line[:MaxLineBytes]
		for !utf8.ValidString(line) && len(line) > 0 {
			line = line[:len(line)-1]
		}
		line += " …[truncated]"
	}
	h.mu.Lock()
	line = h.red.Line(line)
	e := Entry{ID: h.nextID.Add(1), Service: service, Time: time.Now(), Stream: stream, Level: DetectLevel(line), Line: line}
	if stream == "system" && e.Level == "" {
		e.Level = "info"
	}
	r := h.rings[service]
	if r == nil {
		r = &ring{buf: make([]Entry, h.capacity)}
		h.rings[service] = r
	}
	r.push(e)
	for _, s := range h.subs {
		if len(s.services) > 0 && !s.services[service] {
			continue
		}
		select {
		case s.ch <- e:
		default: // slow consumer: drop rather than block the process pipe
			h.dropped.Add(1)
		}
	}
	p := h.persist
	h.mu.Unlock()
	if p != nil {
		select {
		case p <- e:
		default:
			h.dropped.Add(1)
		}
	}
	return e
}

// Dropped returns how many entries were dropped for slow consumers/persistence.
func (h *Hub) Dropped() uint64 { return h.dropped.Load() }

// Recent returns up to n newest entries for the given services (all when
// empty), ordered by ID.
func (h *Hub) Recent(n int, services ...string) []Entry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []Entry
	for name, r := range h.rings {
		if len(services) > 0 && !has(services, name) {
			continue
		}
		out = append(out, r.snapshot()...)
	}
	sortByID(out)
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// Services lists services that have produced output.
func (h *Hub) Services() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var s []string
	for k := range h.rings {
		s = append(s, k)
	}
	return s
}

// Clear discards the in-memory buffer of a service (all when empty).
func (h *Hub) Clear(services ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for name := range h.rings {
		if len(services) == 0 || has(services, name) {
			delete(h.rings, name)
		}
	}
}

// Subscribe streams new entries. Cancel must be called to release resources.
func (h *Hub) Subscribe(services ...string) (<-chan Entry, func()) {
	s := &sub{ch: make(chan Entry, 512), services: map[string]bool{}}
	for _, x := range services {
		s.services[x] = true
	}
	h.mu.Lock()
	h.subSeq++
	id := h.subSeq
	h.subs[id] = s
	h.mu.Unlock()
	var once sync.Once
	return s.ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, id)
			h.mu.Unlock()
		})
	}
}

// Capture reads r line by line until EOF, appending each line. Overlong lines
// are truncated without unbounded buffering.
func (h *Hub) Capture(service, stream string, r io.Reader) {
	br := bufio.NewReaderSize(r, 64*1024)
	var acc []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if len(acc)+len(chunk) <= MaxLineBytes {
			acc = append(acc, chunk...)
		} else if len(acc) < MaxLineBytes {
			acc = append(acc, chunk[:MaxLineBytes-len(acc)]...)
		}
		switch {
		case err == nil:
			h.Append(service, stream, string(acc))
			acc = acc[:0]
		case errors.Is(err, bufio.ErrBufferFull):
			continue // keep accumulating (bounded above) until newline
		default:
			if len(acc) > 0 {
				h.Append(service, stream, string(acc))
			}
			return
		}
	}
}

// StartPersistence begins batched writes to SQLite until ctx ends. The final
// batch is flushed before the returned channel closes.
func (h *Hub) StartPersistence(ctx context.Context, db *sql.DB) <-chan struct{} {
	ch := make(chan Entry, 4096)
	h.mu.Lock()
	h.persist = ch
	h.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		var batch []Entry
		t := time.NewTicker(750 * time.Millisecond)
		defer t.Stop()
		flush := func() {
			if len(batch) == 0 {
				return
			}
			_ = writeBatch(db, batch)
			batch = batch[:0]
		}
		for {
			select {
			case e := <-ch:
				batch = append(batch, e)
				if len(batch) >= 200 {
					flush()
				}
			case <-t.C:
				flush()
			case <-ctx.Done():
				h.mu.Lock()
				h.persist = nil
				h.mu.Unlock()
				for {
					select {
					case e := <-ch:
						batch = append(batch, e)
					default:
						flush()
						return
					}
				}
			}
		}
	}()
	return done
}

func writeBatch(db *sql.DB, batch []Entry) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	st, err := tx.Prepare(`INSERT INTO logs(service,ts,stream,level,line) VALUES(?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer st.Close()
	for _, e := range batch {
		if _, err := st.Exec(e.Service, e.Time.UnixMilli(), e.Stream, e.Level, e.Line); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Query reads persisted history (newest last).
type Query struct {
	Services []string
	Since    time.Time
	Search   string
	Level    string
	Limit    int
}

// QueryDB returns persisted entries matching q.
func QueryDB(db *sql.DB, q Query) ([]Entry, error) {
	if q.Limit <= 0 || q.Limit > 50000 {
		q.Limit = 1000
	}
	var where []string
	var args []any
	if len(q.Services) > 0 {
		where = append(where, "service IN ("+strings.TrimSuffix(strings.Repeat("?,", len(q.Services)), ",")+")")
		for _, s := range q.Services {
			args = append(args, s)
		}
	}
	if !q.Since.IsZero() {
		where = append(where, "ts >= ?")
		args = append(args, q.Since.UnixMilli())
	}
	if q.Search != "" {
		where = append(where, "line LIKE ? ESCAPE '\\'")
		args = append(args, "%"+likeEscape(q.Search)+"%")
	}
	if q.Level != "" {
		where = append(where, "level = ?")
		args = append(args, q.Level)
	}
	sqlq := "SELECT id,service,ts,stream,level,line FROM logs"
	if len(where) > 0 {
		sqlq += " WHERE " + strings.Join(where, " AND ")
	}
	sqlq += " ORDER BY id DESC LIMIT ?"
	args = append(args, q.Limit)
	rows, err := db.Query(sqlq, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var ts int64
		if err := rows.Scan(&e.ID, &e.Service, &ts, &e.Stream, &e.Level, &e.Line); err != nil {
			return nil, err
		}
		e.Time = time.UnixMilli(ts)
		out = append(out, e)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func has(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func sortByID(e []Entry) { sort.Slice(e, func(i, j int) bool { return e[i].ID < e[j].ID }) }
