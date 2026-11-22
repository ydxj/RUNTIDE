package receipts

// Code generated from internal/receiptcore; the receipt format, verification, signing,
// comparison and reports live in receiptcore (platform independent). These aliases keep
// the long-standing receipts.X names working for the CLI, desktop app and MCP server.

import "github.com/runtide/runtide/internal/receiptcore"

type (
	CommandChange   = receiptcore.CommandChange
	CommandsDiff    = receiptcore.CommandsDiff
	CompareMeta     = receiptcore.CompareMeta
	Comparison      = receiptcore.Comparison
	DurationDiff    = receiptcore.DurationDiff
	EnvDiff         = receiptcore.EnvDiff
	ExecutionDoc    = receiptcore.ExecutionDoc
	FailureDiff     = receiptcore.FailureDiff
	FileEntry       = receiptcore.FileEntry
	FilesDiff       = receiptcore.FilesDiff
	GitPin          = receiptcore.GitPin
	KeyPair         = receiptcore.KeyPair
	Manifest        = receiptcore.Manifest
	Receipt         = receiptcore.Receipt
	Redaction       = receiptcore.Redaction
	Reproduction    = receiptcore.Reproduction
	ServiceDiff     = receiptcore.ServiceDiff
	SignatureInfo   = receiptcore.SignatureInfo
	StatusChange    = receiptcore.StatusChange
	ToolDiff        = receiptcore.ToolDiff
	VerifDiff       = receiptcore.VerifDiff
	Verification    = receiptcore.Verification
	VerificationDoc = receiptcore.VerificationDoc
	VerifyOptions   = receiptcore.VerifyOptions
)

const (
	DirArtifacts       = receiptcore.DirArtifacts
	DirLogs            = receiptcore.DirLogs
	Extension          = receiptcore.Extension
	FCommands          = receiptcore.FCommands
	FDiffs             = receiptcore.FDiffs
	FEnvironment       = receiptcore.FEnvironment
	FEvents            = receiptcore.FEvents
	FExecution         = receiptcore.FExecution
	FFileChanges       = receiptcore.FFileChanges
	FManifest          = receiptcore.FManifest
	FProcesses         = receiptcore.FProcesses
	FReproduction      = receiptcore.FReproduction
	FServices          = receiptcore.FServices
	FSignature         = receiptcore.FSignature
	FVerification      = receiptcore.FVerification
	FormatName         = receiptcore.FormatName
	FormatVersion      = receiptcore.FormatVersion
	MaxCompressionRate = receiptcore.MaxCompressionRate
	MaxEntries         = receiptcore.MaxEntries
	MaxEntryBytes      = receiptcore.MaxEntryBytes
	MaxManifestBytes   = receiptcore.MaxManifestBytes
	MaxTotalBytes      = receiptcore.MaxTotalBytes
	ReproFull          = receiptcore.ReproFull
	ReproMetadataOnly  = receiptcore.ReproMetadataOnly
	ReproNone          = receiptcore.ReproNone
	ReproPartial       = receiptcore.ReproPartial
)

var (
	Compare     = receiptcore.Compare
	Fingerprint = receiptcore.Fingerprint
	GenerateKey = receiptcore.GenerateKey
	HTML        = receiptcore.HTML
	Import      = receiptcore.Import
	Load        = receiptcore.Load
	LoadKey     = receiptcore.LoadKey
	ReadLog     = receiptcore.ReadLog
	ReceiptsDir = receiptcore.ReceiptsDir
	SafeName    = receiptcore.SafeName
	Verify      = receiptcore.Verify
)
