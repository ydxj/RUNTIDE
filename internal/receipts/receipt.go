// Package receipts implements the RUNTIDE Run Receipt: a portable, versioned,
// integrity-checked archive of an execution's evidence. A receipt is evidence,
// not a machine snapshot: see docs/RUN_RECEIPTS.md for what it can and cannot prove.
package receipts

import (
	"time"

	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/execution"
	"github.com/runtide/runtide/internal/store"
	"github.com/runtide/runtide/internal/verification"
)

// Format identification.
const (
	FormatName    = "runtide.receipt"
	FormatVersion = 1
	Extension     = ".runtide.zip"
)

// Reproducibility classes.
const (
	ReproFull         = "fully_reconstructable"
	ReproPartial      = "partially_reconstructable"
	ReproMetadataOnly = "metadata_only"
	ReproNone         = "not_reproducible"
)

// FileEntry is one evidence file listed in the manifest.
type FileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest is manifest.json: it lists every other file with size and SHA-256.
type Manifest struct {
	Format          string      `json:"format"`
	FormatVersion   int         `json:"formatVersion"`
	RuntideVersion  string      `json:"runtideVersion"`
	ExecutionID     string      `json:"executionId"`
	ProjectID       string      `json:"projectId"`
	ProjectName     string      `json:"projectName"`
	Task            string      `json:"task"`
	CreatedAt       time.Time   `json:"createdAt"`
	Status          string      `json:"status"`
	Verification    string      `json:"verification"`
	Isolation       string      `json:"isolation"`
	Reproducibility string      `json:"reproducibility"`
	Warnings        []string    `json:"warnings,omitempty"`
	Redaction       Redaction   `json:"redaction"`
	Files           []FileEntry `json:"files"`
}

// Redaction summarises what was withheld from the receipt.
type Redaction struct {
	CommandsWithMaskedArgs int  `json:"commandsWithMaskedArgs"`
	SensitiveFilesExcluded int  `json:"sensitiveFilesExcluded"`
	EnvValuesIncluded      bool `json:"envValuesIncluded"` // false: only variable names are recorded
	DiffsIncluded          bool `json:"diffsIncluded"`
	AbsolutePathsRemoved   bool `json:"absolutePathsRemoved"`
}

// Reproduction describes how much of the execution can be reconstructed.
type Reproduction struct {
	Class      string   `json:"class"`
	Reasons    []string `json:"reasons"`
	Missing    []string `json:"missing"`
	Replayable []string `json:"replayableSteps"`
	Git        *GitPin  `json:"git,omitempty"`
}

// GitPin identifies the source state the execution started from.
type GitPin struct {
	Commit string `json:"commit"`
	Branch string `json:"branch,omitempty"`
	Clean  bool   `json:"clean"`
}

// ExecutionDoc is execution.json (no absolute local paths).
type ExecutionDoc struct {
	Execution  *domain.Execution `json:"execution"`
	Project    string            `json:"project"`
	Workspace  string            `json:"workspace"`
	ConfigYAML string            `json:"workspaceConfigYaml"` // workspace configuration with sensitive env values masked
	DurationMs int64             `json:"durationMs"`
	Isolation  string            `json:"isolation"`
}

// VerificationDoc is verification.json.
type VerificationDoc struct {
	Summary verification.Summary        `json:"summary"`
	Results []domain.VerificationResult `json:"results"`
}

// Receipt is a fully parsed receipt (after integrity verification).
type Receipt struct {
	Manifest     Manifest                     `json:"manifest"`
	Execution    ExecutionDoc                 `json:"execution"`
	Environment  execution.Environment        `json:"environment"`
	Commands     []domain.Command             `json:"commands"`
	Events       []domain.ExecEvent           `json:"events"`
	Processes    []store.ManagedProcessRecord `json:"processes"`
	Services     []store.ServiceRecord        `json:"services"`
	FileChanges  []domain.FileChange          `json:"fileChanges"`
	Verification VerificationDoc              `json:"verification"`
	Reproduction Reproduction                 `json:"reproduction"`
	LogFiles     map[string]string            `json:"logFiles"` // command id -> archive path
	Diffs        map[string]string            `json:"diffs,omitempty"`
	Signature    *SignatureInfo               `json:"signature,omitempty"`
	SHA256       string                       `json:"sha256"` // of the archive file
}

// Archive entry names.
const (
	fManifest     = "manifest.json"
	fExecution    = "execution.json"
	fEnvironment  = "environment.json"
	fCommands     = "commands.json"
	fEvents       = "events.json"
	fProcesses    = "processes.json"
	fServices     = "services.json"
	fFileChanges  = "file-changes.json"
	fVerification = "verification.json"
	fReproduction = "reproduction.json"
	fDiffs        = "file-diffs.json"
	fSignature    = "signature.json"
	dirLogs       = "logs/"
	dirArtifacts  = "artifacts/"
)
