// Package redact masks secrets before they reach logs, the UI or exports.
package redact

import (
	"regexp"
	"strings"
)

const Mask = "«redacted»"

var sensitiveKey = regexp.MustCompile(`(?i)(pass(word|wd)?|secret|token|api[_-]?key|private[_-]?key|credential|auth|session|cookie|signature|access[_-]?key|client[_-]?secret|dsn|connection[_-]?string)`)

// valuePatterns match credential shapes that appear in free text.
var valuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

// URL credentials: scheme://user:pass@host
var urlCreds = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+:)([^\s@/]+)(@)`)

// key=value or key: value / "key":"value"
var assignment = regexp.MustCompile(`(?i)(["']?[A-Za-z0-9_.-]*(?:pass(?:word|wd)?|secret|token|api[_-]?key|private[_-]?key|credential|access[_-]?key)[A-Za-z0-9_.-]*["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;&]+)`)

// Redactor applies redaction with optional user-defined extra patterns.
type Redactor struct {
	extra   []*regexp.Regexp
	Enabled bool
}

// New returns an enabled redactor.
func New(extraPatterns ...string) *Redactor {
	r := &Redactor{Enabled: true}
	for _, p := range extraPatterns {
		if re, err := regexp.Compile(p); err == nil {
			r.extra = append(r.extra, re)
		}
	}
	return r
}

// Line redacts one line of text.
func (r *Redactor) Line(s string) string {
	if r != nil && !r.Enabled {
		return s
	}
	s = urlCreds.ReplaceAllString(s, "${1}"+Mask+"${3}")
	s = assignment.ReplaceAllString(s, "${1}"+Mask)
	for _, re := range valuePatterns {
		s = re.ReplaceAllString(s, Mask)
	}
	if r != nil {
		for _, re := range r.extra {
			s = re.ReplaceAllString(s, Mask)
		}
	}
	return s
}

// IsSensitiveKey reports whether an environment variable name looks secret.
func IsSensitiveKey(name string) bool { return sensitiveKey.MatchString(name) }

// Value returns Mask for sensitive keys, the value otherwise.
func Value(name, value string) string {
	if IsSensitiveKey(name) && value != "" {
		return Mask
	}
	return value
}

// Env masks sensitive values in a KEY=VALUE slice (for display only).
func Env(env []string) []string {
	out := make([]string, len(env))
	for i, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			out[i] = kv
			continue
		}
		out[i] = k + "=" + Value(k, v)
	}
	return out
}
