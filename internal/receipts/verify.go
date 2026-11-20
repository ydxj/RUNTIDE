package receipts

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/runtide/runtide/internal/store"
)

// Limits protect against hostile archives (zip bombs, path tricks, huge manifests).
const (
	MaxEntries         = 5000
	MaxEntryBytes      = 256 << 20
	MaxTotalBytes      = 1 << 30
	MaxManifestBytes   = 8 << 20
	MaxCompressionRate = 200 // uncompressed/compressed ratio above which large entries are rejected
)

// VerifyOptions configure verification.
type VerifyOptions struct {
	TrustedKeys []string // accepted signer fingerprints
}

// Verification is the outcome of checking a receipt file.
type Verification struct {
	Path         string         `json:"path"`
	SHA256       string         `json:"sha256"`
	Valid        bool           `json:"valid"`
	Problems     []string       `json:"problems"`
	Warnings     []string       `json:"warnings"`
	FilesChecked int            `json:"filesChecked"`
	Manifest     *Manifest      `json:"manifest,omitempty"`
	Signature    *SignatureInfo `json:"signature,omitempty"`
	Authenticity string         `json:"authenticity"` // what the checks do and do not prove
}

// SafeName reports whether an archive entry name is a plain relative path.
func SafeName(name string) bool {
	if name == "" || strings.ContainsAny(name, "\x00\\:") || strings.HasPrefix(name, "/") {
		return false
	}
	clean := path.Clean(name)
	if clean != strings.TrimSuffix(name, "/") || strings.HasPrefix(clean, "../") || clean == ".." || clean == "." {
		return false
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." || seg == "" || strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") {
			return false
		}
	}
	return true
}

// readEntry reads at most limit bytes of a zip entry and fails if it is larger.
func readEntry(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("entry %s exceeds %d bytes", f.Name, limit)
	}
	return b, nil
}

// open reads and structurally validates the archive; the returned entries map
// holds parsed manifest and file handles by name. Nothing is extracted to disk.
func open(p string) (*zip.ReadCloser, map[string]*zip.File, []string, string, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, nil, nil, "", err
	}
	if st.IsDir() {
		return nil, nil, nil, "", errors.New("receipt path is a directory")
	}
	h := sha256.New()
	f, err := os.Open(p)
	if err != nil {
		return nil, nil, nil, "", err
	}
	if _, err := io.Copy(h, f); err != nil {
		f.Close()
		return nil, nil, nil, "", err
	}
	f.Close()
	sum := hex.EncodeToString(h.Sum(nil))
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, nil, nil, sum, fmt.Errorf("not a valid zip archive: %w", err)
	}
	var problems []string
	entries := map[string]*zip.File{}
	if len(zr.File) > MaxEntries {
		problems = append(problems, fmt.Sprintf("archive has %d entries (limit %d)", len(zr.File), MaxEntries))
		return zr, entries, problems, sum, nil
	}
	var total uint64
	seen := map[string]bool{}
	for _, e := range zr.File {
		name := e.Name
		lower := strings.ToLower(name)
		switch {
		case !SafeName(name):
			problems = append(problems, fmt.Sprintf("unsafe entry name %q", name))
			continue
		case seen[lower]:
			problems = append(problems, fmt.Sprintf("duplicate entry %q", name))
			continue
		case e.Mode()&os.ModeSymlink != 0:
			problems = append(problems, fmt.Sprintf("entry %q is a symbolic link", name))
			continue
		case e.FileInfo().IsDir():
			continue
		case e.UncompressedSize64 > MaxEntryBytes:
			problems = append(problems, fmt.Sprintf("entry %q is larger than %d MiB", name, MaxEntryBytes>>20))
			continue
		case e.UncompressedSize64 > 1<<20 && e.CompressedSize64 > 0 && e.UncompressedSize64/e.CompressedSize64 > MaxCompressionRate:
			problems = append(problems, fmt.Sprintf("entry %q has a suspicious compression ratio (possible zip bomb)", name))
			continue
		}
		seen[lower] = true
		total += e.UncompressedSize64
		if total > MaxTotalBytes {
			problems = append(problems, "archive expands to more than the allowed total size")
			break
		}
		entries[name] = e
	}
	return zr, entries, problems, sum, nil
}

// Verify checks structure, manifest, every file's size and SHA-256, and the
// optional signature. It never executes or extracts anything.
func Verify(p string, opts VerifyOptions) (*Verification, error) {
	zr, entries, problems, sum, err := open(p)
	v := &Verification{Path: p, SHA256: sum, Problems: problems, Warnings: []string{}}
	if err != nil {
		v.Problems = append(v.Problems, err.Error())
		return v, nil
	}
	defer func() { _ = zr.Close() }()
	v.Authenticity = "Integrity checks show the files match the manifest. They do not prove who created the receipt or that its contents are true unless a signature from a key you trust is present."
	mfEntry := entries[fManifest]
	if mfEntry == nil {
		v.Problems = append(v.Problems, "manifest.json is missing")
		return v, nil
	}
	mfBytes, err := readEntry(mfEntry, MaxManifestBytes)
	if err != nil {
		v.Problems = append(v.Problems, err.Error())
		return v, nil
	}
	var mf Manifest
	if err := json.Unmarshal(mfBytes, &mf); err != nil {
		v.Problems = append(v.Problems, "manifest.json is not valid JSON: "+err.Error())
		return v, nil
	}
	v.Manifest = &mf
	if mf.Format != FormatName {
		v.Problems = append(v.Problems, fmt.Sprintf("unknown format %q", mf.Format))
	}
	if mf.FormatVersion < 1 || mf.FormatVersion > FormatVersion {
		v.Problems = append(v.Problems, fmt.Sprintf("receipt format version %d is not supported by this RUNTIDE (supports up to %d)", mf.FormatVersion, FormatVersion))
	}
	listed := map[string]bool{fManifest: true, fSignature: true}
	for _, fe := range mf.Files {
		if !SafeName(fe.Path) {
			v.Problems = append(v.Problems, fmt.Sprintf("manifest lists unsafe path %q", fe.Path))
			continue
		}
		if listed[fe.Path] {
			v.Problems = append(v.Problems, fmt.Sprintf("manifest lists %q more than once", fe.Path))
			continue
		}
		listed[fe.Path] = true
		e := entries[fe.Path]
		if e == nil {
			v.Problems = append(v.Problems, fmt.Sprintf("listed file %q is missing from the archive", fe.Path))
			continue
		}
		b, err := readEntry(e, MaxEntryBytes)
		if err != nil {
			v.Problems = append(v.Problems, err.Error())
			continue
		}
		s := sha256.Sum256(b)
		if int64(len(b)) != fe.Size {
			v.Problems = append(v.Problems, fmt.Sprintf("%s: size %d does not match manifest (%d)", fe.Path, len(b), fe.Size))
		} else if hex.EncodeToString(s[:]) != fe.SHA256 {
			v.Problems = append(v.Problems, fmt.Sprintf("%s: SHA-256 does not match manifest (file was modified)", fe.Path))
		}
		v.FilesChecked++
	}
	var extra []string
	for n := range entries {
		if !listed[n] {
			extra = append(extra, n)
		}
	}
	sort.Strings(extra)
	for _, n := range extra {
		v.Problems = append(v.Problems, fmt.Sprintf("archive contains %q which the manifest does not list", n))
	}
	if sig := entries[fSignature]; sig != nil {
		sb, err := readEntry(sig, 1<<16)
		if err != nil {
			v.Problems = append(v.Problems, err.Error())
		} else if info, err := checkSignature(sb, mfBytes, opts.TrustedKeys); err != nil {
			v.Problems = append(v.Problems, err.Error())
		} else {
			v.Signature = info
			if !info.Valid {
				v.Problems = append(v.Problems, "signature does not match the manifest")
			} else if !info.Trusted {
				v.Warnings = append(v.Warnings, fmt.Sprintf("signed by key %s, which is not in your trusted list; the signature only proves the same key signed it", info.Fingerprint))
			}
		}
	} else {
		v.Warnings = append(v.Warnings, "receipt is not signed")
	}
	v.Warnings = append(v.Warnings, mf.Warnings...)
	v.Valid = len(v.Problems) == 0
	return v, nil
}

// Load verifies and parses a receipt. It fails if verification found problems.
func Load(p string, opts VerifyOptions) (*Receipt, *Verification, error) {
	v, err := Verify(p, opts)
	if err != nil {
		return nil, v, err
	}
	if !v.Valid {
		return nil, v, fmt.Errorf("receipt failed verification: %s", strings.Join(v.Problems, "; "))
	}
	zr, entries, _, _, err := open(p)
	if err != nil {
		return nil, v, err
	}
	defer func() { _ = zr.Close() }()
	r := &Receipt{Manifest: *v.Manifest, SHA256: v.SHA256, Signature: v.Signature, LogFiles: map[string]string{}}
	get := func(name string, into any) error {
		e := entries[name]
		if e == nil {
			return fmt.Errorf("%s is missing", name)
		}
		b, err := readEntry(e, MaxEntryBytes)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, into); err != nil {
			return fmt.Errorf("%s is malformed: %w", name, err)
		}
		return nil
	}
	for _, step := range []struct {
		n string
		v any
	}{
		{fExecution, &r.Execution}, {fEnvironment, &r.Environment}, {fCommands, &r.Commands}, {fEvents, &r.Events},
		{fProcesses, &r.Processes}, {fServices, &r.Services}, {fFileChanges, &r.FileChanges}, {fVerification, &r.Verification}, {fReproduction, &r.Reproduction},
	} {
		if err := get(step.n, step.v); err != nil {
			return nil, v, err
		}
	}
	if entries[fDiffs] != nil {
		_ = get(fDiffs, &r.Diffs)
	}
	for _, c := range r.Commands {
		prefix := fmt.Sprintf("%s%03d-", dirLogs, c.Seq)
		for n := range entries {
			if strings.HasPrefix(n, prefix) {
				r.LogFiles[c.ID] = n
			}
		}
	}
	return r, v, nil
}

// ReadLog returns a command's log text from the receipt file.
func ReadLog(p string, logPath string) (string, error) {
	if !SafeName(logPath) || !strings.HasPrefix(logPath, dirLogs) {
		return "", errors.New("not a log path")
	}
	zr, entries, _, _, err := open(p)
	if err != nil {
		return "", err
	}
	defer func() { _ = zr.Close() }()
	e := entries[logPath]
	if e == nil {
		return "", errors.New("log not found in receipt")
	}
	b, err := readEntry(e, MaxEntryBytes)
	return string(b), err
}

// ReceiptsDir is where imported receipts are kept below the data directory.
func ReceiptsDir(home string) string { return filepath.Join(home, "receipts") }

// Import verifies a receipt and indexes a copy of it. It never executes
// anything from the receipt and never trusts archive paths (nothing is
// extracted: the verified archive is copied whole under a name derived from
// its own hash).
func Import(st *store.Store, home, src string, opts VerifyOptions) (*store.ReceiptRecord, *Verification, error) {
	v, err := Verify(src, opts)
	if err != nil {
		return nil, v, err
	}
	if !v.Valid {
		return nil, v, fmt.Errorf("receipt failed verification: %s", strings.Join(v.Problems, "; "))
	}
	dir := ReceiptsDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, v, err
	}
	dst := filepath.Join(dir, v.SHA256+Extension)
	if _, err := os.Stat(dst); err != nil {
		in, err := os.ReadFile(src)
		if err != nil {
			return nil, v, err
		}
		if err := os.WriteFile(dst, in, 0o644); err != nil {
			return nil, v, err
		}
	}
	mj, _ := json.Marshal(v.Manifest)
	rec := &store.ReceiptRecord{ExecutionID: v.Manifest.ExecutionID, ProjectName: v.Manifest.ProjectName, Path: dst, SHA256: v.SHA256, Imported: true,
		Status: v.Manifest.Status, ManifestJSON: string(mj)}
	if fi, err := os.Stat(dst); err == nil {
		rec.Size = fi.Size()
	}
	if v.Signature != nil && v.Signature.Valid {
		rec.SignedBy = v.Signature.Fingerprint
	}
	if err := st.AddReceipt(rec); err != nil {
		return nil, v, err
	}
	st.Audit("local", "receipt.import", rec.ID, v.SHA256[:12])
	return rec, v, nil
}
