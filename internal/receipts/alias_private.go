package receipts

import "github.com/runtide/runtide/internal/receiptcore"

// Unexported names the builder and the replay code use for archive layout.
const (
	fManifest     = receiptcore.FManifest
	fExecution    = receiptcore.FExecution
	fEnvironment  = receiptcore.FEnvironment
	fCommands     = receiptcore.FCommands
	fEvents       = receiptcore.FEvents
	fProcesses    = receiptcore.FProcesses
	fServices     = receiptcore.FServices
	fFileChanges  = receiptcore.FFileChanges
	fVerification = receiptcore.FVerification
	fReproduction = receiptcore.FReproduction
	fDiffs        = receiptcore.FDiffs
	fSignature    = receiptcore.FSignature
	dirLogs       = receiptcore.DirLogs
	dirArtifacts  = receiptcore.DirArtifacts
)
