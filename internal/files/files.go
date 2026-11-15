// Package files tracks what changed in a workspace between two points in time.
// It respects .gitignore (through git when the directory is a repository),
// never records sensitive files, and never captures content of large files.
package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/runtide/runtide/internal/domain"
)

// DefaultExcludedDirs are never descended into or reported.
var DefaultExcludedDirs = []string{
	".git", ".runtide", "node_modules", "vendor", "dist", "build", "target", "out", ".next", ".nuxt", ".svelte-kit",
	".venv", "venv", "__pycache__", ".pytest_cache", ".mypy_cache", ".gradle", ".idea", ".vs", "coverage", ".cache", ".turbo",
}

// sensitiveGlobs match base names that may contain secrets. They are excluded
// from snapshots, baselines, diffs and receipts regardless of configuration.
var sensitiveGlobs = []string{
	".env", ".env.*", "*.pem", "*.key", "*.pfx", "*.p12", "*.jks", "*.keystore", "id_rsa*", "id_dsa*", "id_ecdsa*", "id_ed25519*",
	"credentials", "credentials.*", "*.credentials", "secrets.*", "*secret*.json", "*.kdbx", ".npmrc", ".pypirc", ".netrc", ".htpasswd",
	"service-account*.json", "*.tfstate", "*.tfvars",
}

// SensitivePath reports whether a relative path looks like it holds secrets.
func SensitivePath(rel string) bool {
	base := strings.ToLower(path.Base(filepath.ToSlash(rel)))
	if strings.HasSuffix(base, ".example") || strings.HasSuffix(base, ".sample") || strings.HasSuffix(base, ".template") {
		return false
	}
	for _, g := range sensitiveGlobs {
		if ok, _ := path.Match(g, base); ok {
			return true
		}
	}
	return false
}

// Options configure a snapshot.
type Options struct {
	Root        string
	Exclude     []string // extra globs, matched against the slash-separated relative path and its base name
	MaxFiles    int      // default 100000
	MaxHashSize int64    // files larger than this are listed without a content hash (default 16 MiB)
}

// Info is the recorded state of one file.
type Info struct {
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"` // unix ms
	Hash    string `json:"hash,omitempty"`
}

// Snapshot is the set of tracked files at a moment.
type Snapshot struct {
	Taken     time.Time       `json:"taken"`
	Files     map[string]Info `json:"files"`
	Skipped   map[string]int  `json:"skipped"` // reason -> count (names are not kept)
	Truncated bool            `json:"truncated"`
	UsedGit   bool            `json:"usedGit"`
}

func excluded(rel string, extra []string) (string, bool) {
	rel = filepath.ToSlash(rel)
	for _, seg := range strings.Split(rel, "/") {
		for _, d := range DefaultExcludedDirs {
			if seg == d {
				return "generated-or-dependency-dir", true
			}
		}
	}
	if SensitivePath(rel) {
		return "sensitive", true
	}
	for _, g := range extra {
		g = filepath.ToSlash(g)
		if ok, _ := path.Match(g, rel); ok {
			return "configured-exclusion", true
		}
		if ok, _ := path.Match(g, path.Base(rel)); ok {
			return "configured-exclusion", true
		}
		if strings.HasSuffix(g, "/") && strings.HasPrefix(rel+"/", g) {
			return "configured-exclusion", true
		}
	}
	return "", false
}

// IsGitRepo reports whether dir is inside a git work tree.
func IsGitRepo(dir string) bool {
	out, err := gitOut(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}

func listPaths(opts Options) (paths []string, usedGit bool, err error) {
	if IsGitRepo(opts.Root) {
		// tracked + untracked, minus everything .gitignore excludes
		out, gerr := gitOut(opts.Root, "ls-files", "-z", "-c", "-o", "--exclude-standard", "--", ".")
		if gerr == nil {
			for _, p := range strings.Split(out, "\x00") {
				if p != "" {
					paths = append(paths, p)
				}
			}
			return paths, true, nil
		}
	}
	err = filepath.WalkDir(opts.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		rel, rerr := filepath.Rel(opts.Root, p)
		if rerr != nil || rel == "." {
			return nil
		}
		if d.IsDir() {
			for _, x := range DefaultExcludedDirs {
				if d.Name() == x {
					return filepath.SkipDir
				}
			}
			return nil
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	return paths, false, err
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Take records the tracked files under opts.Root.
func Take(opts Options) (*Snapshot, error) {
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = 100000
	}
	if opts.MaxHashSize <= 0 {
		opts.MaxHashSize = 16 << 20
	}
	paths, usedGit, err := listPaths(opts)
	if err != nil {
		return nil, err
	}
	s := &Snapshot{Taken: time.Now(), Files: map[string]Info{}, Skipped: map[string]int{}, UsedGit: usedGit}
	for _, rel := range paths {
		if reason, ex := excluded(rel, opts.Exclude); ex {
			s.Skipped[reason]++
			continue
		}
		if len(s.Files) >= opts.MaxFiles {
			s.Truncated = true
			break
		}
		full := filepath.Join(opts.Root, filepath.FromSlash(rel))
		st, err := os.Lstat(full)
		if err != nil || !st.Mode().IsRegular() {
			continue // deleted tracked file, symlink, device...
		}
		info := Info{Size: st.Size(), ModTime: st.ModTime().UnixMilli()}
		if st.Size() <= opts.MaxHashSize {
			if h, err := hashFile(full); err == nil {
				info.Hash = h
			}
		}
		s.Files[filepath.ToSlash(rel)] = info
	}
	return s, nil
}

// Diff compares two snapshots. A deleted and an added file with identical
// content hash are reported as one rename.
func Diff(before, after *Snapshot) []domain.FileChange {
	var out []domain.FileChange
	added := map[string]Info{}
	deleted := map[string]Info{}
	for p, a := range after.Files {
		b, ok := before.Files[p]
		if !ok {
			added[p] = a
			continue
		}
		if a.Hash != b.Hash || (a.Hash == "" && (a.Size != b.Size || a.ModTime != b.ModTime)) {
			out = append(out, domain.FileChange{Path: p, Change: "modified", SizeBefore: b.Size, SizeAfter: a.Size, HashBefore: b.Hash, HashAfter: a.Hash, ModTime: ts(a.ModTime)})
		}
	}
	for p, b := range before.Files {
		if _, ok := after.Files[p]; !ok {
			deleted[p] = b
		}
	}
	byHash := map[string]string{}
	for p, b := range deleted {
		if b.Hash != "" {
			byHash[b.Hash] = p
		}
	}
	for p, a := range added {
		if old, ok := byHash[a.Hash]; ok && a.Hash != "" {
			out = append(out, domain.FileChange{Path: p, Change: "renamed", OldPath: old, SizeBefore: deleted[old].Size, SizeAfter: a.Size, HashBefore: a.Hash, HashAfter: a.Hash, ModTime: ts(a.ModTime)})
			delete(deleted, old)
			delete(byHash, a.Hash)
			continue
		}
		out = append(out, domain.FileChange{Path: p, Change: "added", SizeAfter: a.Size, HashAfter: a.Hash, ModTime: ts(a.ModTime)})
	}
	for p, b := range deleted {
		out = append(out, domain.FileChange{Path: p, Change: "deleted", SizeBefore: b.Size, HashBefore: b.Hash})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func ts(ms int64) string { return time.UnixMilli(ms).UTC().Format(time.RFC3339) }

// Baselines returns the content of small, non-sensitive text files so
// modifications can be shown as diffs later. Total size is bounded.
func Baselines(root string, s *Snapshot, perFile, total int64) map[string][]byte {
	if perFile <= 0 {
		perFile = 256 << 10
	}
	if total <= 0 {
		total = 32 << 20
	}
	out := map[string][]byte{}
	var used int64
	paths := make([]string, 0, len(s.Files))
	for p := range s.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		info := s.Files[p]
		if info.Size > perFile || used+info.Size > total {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil || !IsText(b) {
			continue
		}
		out[p] = b
		used += int64(len(b))
	}
	return out
}

// IsText reports whether content looks like UTF-8 text (no NUL bytes).
func IsText(b []byte) bool { return utf8.Valid(b) && !bytes.Contains(b, []byte{0}) }

// GitInfo describes the repository state of a directory.
type GitInfo struct {
	Repo   bool     `json:"repo"`
	Root   string   `json:"root,omitempty"`
	Head   string   `json:"head,omitempty"`
	Branch string   `json:"branch,omitempty"`
	Dirty  bool     `json:"dirty"`
	Status []string `json:"status,omitempty"` // `git status --porcelain` lines (paths only; sensitive ones omitted)
}

// Git inspects dir without changing anything.
func Git(dir string) GitInfo {
	if !IsGitRepo(dir) {
		return GitInfo{}
	}
	g := GitInfo{Repo: true}
	if o, err := gitOut(dir, "rev-parse", "--show-toplevel"); err == nil {
		g.Root = filepath.FromSlash(strings.TrimSpace(o))
	}
	if o, err := gitOut(dir, "rev-parse", "HEAD"); err == nil {
		g.Head = strings.TrimSpace(o)
	}
	if o, err := gitOut(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		g.Branch = strings.TrimSpace(o)
	}
	if o, err := gitOut(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"); err == nil {
		for _, rec := range strings.Split(o, "\x00") {
			if len(rec) > 3 && !SensitivePath(rec[3:]) {
				g.Status = append(g.Status, rec)
			}
			if len(rec) > 3 {
				g.Dirty = true
			}
		}
	}
	return g
}

// UnifiedDiff renders a line diff with three lines of context. It refuses
// inputs whose product of line counts is too large.
func UnifiedDiff(oldName, newName, a, b string) string {
	al, bl := splitLines(a), splitLines(b)
	if len(al)*len(bl) > 6_000_000 {
		return fmt.Sprintf("--- %s\n+++ %s\n(diff too large to render: %d vs %d lines)\n", oldName, newName, len(al), len(bl))
	}
	ops := lcsOps(al, bl)
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", oldName, newName)
	const ctx = 3
	n := len(ops)
	i := 0
	for i < n {
		for i < n && ops[i].kind == ' ' {
			i++
		}
		if i >= n {
			break
		}
		start := max(0, i-ctx)
		end := i
		for end < n {
			j := end
			for j < n && ops[j].kind != ' ' {
				j++
			}
			k := j
			for k < n && ops[k].kind == ' ' && k-j < 2*ctx+1 {
				k++
			}
			if k < n && ops[k].kind != ' ' {
				end = k
				continue
			}
			end = min(n, j+ctx)
			break
		}
		var oa, ob, ca, cb int
		for _, o := range ops[:start] {
			if o.kind != '+' {
				oa++
			}
			if o.kind != '-' {
				ob++
			}
		}
		for _, o := range ops[start:end] {
			if o.kind != '+' {
				ca++
			}
			if o.kind != '-' {
				cb++
			}
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", oa+1, ca, ob+1, cb)
		for _, o := range ops[start:end] {
			sb.WriteByte(o.kind)
			sb.WriteString(o.text)
			sb.WriteByte('\n')
		}
		i = end
	}
	return sb.String()
}

type op struct {
	kind byte // ' ', '-', '+'
	text string
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), "\n")
}

func lcsOps(a, b []string) []op {
	n, m := len(a), len(b)
	t := make([][]int32, n+1)
	for i := range t {
		t[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i][j] = t[i+1][j+1] + 1
			} else if t[i+1][j] >= t[i][j+1] {
				t[i][j] = t[i+1][j]
			} else {
				t[i][j] = t[i][j+1]
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', a[i]})
			i++
			j++
		case t[i+1][j] >= t[i][j+1]:
			ops = append(ops, op{'-', a[i]})
			i++
		default:
			ops = append(ops, op{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', b[j]})
	}
	return ops
}
