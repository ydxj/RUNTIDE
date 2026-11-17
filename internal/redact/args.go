package redact

import "strings"

// Args masks secrets in a command-line argument vector. It returns the masked
// copy and whether anything was masked (such a command can no longer be
// replayed verbatim, which receipts must say).
//
// Masked: values after sensitive flags (--password hunter2), the value part of
// sensitive KEY=value arguments, URL credentials and well-known token formats.
func Args(r *Redactor, args []string) ([]string, bool) {
	out := make([]string, len(args))
	changed := false
	maskNext := false
	for i, a := range args {
		switch {
		case maskNext:
			out[i], changed, maskNext = Mask, true, false
		case strings.HasPrefix(a, "-"):
			name := strings.TrimLeft(a, "-")
			if k, _, hasVal := strings.Cut(name, "="); hasVal {
				if IsSensitiveKey(k) {
					out[i], changed = a[:len(a)-len(name)+len(k)+1]+Mask, true
					continue
				}
			} else if len(name) > 1 && IsSensitiveKey(name) {
				maskNext = true
			}
			out[i] = a
		default:
			m := r.Line(a)
			if m != a {
				changed = true
			}
			out[i] = m
		}
		if out[i] != a && !strings.Contains(out[i], Mask) {
			changed = true
		}
	}
	return out, changed
}
