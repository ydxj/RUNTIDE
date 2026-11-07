# Run Receipt format specification (version 1)

Normative description of the `.runtide.zip` format as implemented in `internal/receiptcore`. For what a receipt can and cannot
prove, read [RUN_RECEIPTS.md](RUN_RECEIPTS.md). Where this document and the code disagree, the code and its tests win; please
report the difference.

## 1. Container

A receipt is a ZIP archive (Deflate or Store), conventionally named `*.runtide.zip`. Entry names are relative, use `/`, and
contain no `..`, empty segments, drive letters, backslashes, NUL, or trailing dots or spaces. Anything else makes the receipt
**invalid** (it is reported as a problem, never extracted).

Limits enforced when reading (hostile-archive protection):

| Limit | Value |
| --- | --- |
| Entries | 5 000 |
| One entry, uncompressed | 256 MiB |
| All entries, uncompressed | 1 GiB |
| Manifest | 8 MiB |
| Compression ratio of an entry above 1 MiB | at most 200 : 1 |

Readers never extract the archive to disk: they stream entries and check them against the manifest.

## 2. Entries

| Entry | Content |
| --- | --- |
| `manifest.json` | the manifest (section 3); always present |
| `execution.json` | execution record, project and workspace names, workspace configuration YAML with sensitive environment values masked, duration, isolation |
| `environment.json` | OS, architecture, RUNTIDE version, isolation, `inheritEnv`, environment **variable names**, tool versions, git state (`repo`, `head`, `branch`, `dirty`), capture time, whether RUNTIDE was elevated |
| `commands.json` | commands: kind, name, executable, arguments (secret-looking values masked, `redacted` flag), cwd (scrubbed), exit code, timing, byte counts, `truncated`, `timedOut`, `cancelled`, error |
| `events.json` | the timeline: `seq`, time, `type`, `source` (`observed`, `reported`, `inferred`), `severity`, `subject`, `data` |
| `processes.json`, `services.json` | managed processes and services observed |
| `file-changes.json` | per-path change kind and hashes; sensitive files appear as excluded, never with content |
| `verification.json` | `summary` (`overall`, counts, reasons) and `results` (per check: name, type, status, required, error, evidence, timing) |
| `reproduction.json` | reproducibility class, reasons, what is missing, replayable steps, git pin |
| `logs/NNN-<name>.log` | redacted output of one command (`NNN` = the command's sequence number), one line per output line, each prefixed `[stdout]` or `[stderr]` |
| `file-diffs.json` | **optional**: text diffs, present only when requested |
| `artifacts/<path>` | **optional**: embedded workspace files, present only when requested and never for sensitive files |
| `signature.json` | **optional**: signature over the manifest (section 5) |

## 3. Manifest

```json
{
  "format": "runtide.receipt",
  "formatVersion": 1,
  "runtideVersion": "0.9.1",
  "executionId": "ex_…", "projectId": "pr_…", "projectName": "shop", "task": "…",
  "createdAt": "2026-11-06T19:00:00Z",
  "status": "COMPLETED",
  "verification": "passed",
  "isolation": "managed",
  "reproducibility": "partially_reconstructable",
  "warnings": ["…"],
  "redaction": {
    "commandsWithMaskedArgs": 0, "sensitiveFilesExcluded": 0,
    "envValuesIncluded": false, "diffsIncluded": false, "absolutePathsRemoved": true
  },
  "files": [ { "path": "commands.json", "size": 1234, "sha256": "…hex…" } ]
}
```

`files` lists **every** entry except `manifest.json` and `signature.json`, with its exact uncompressed size and lowercase hex
SHA-256. `status` is an execution state, `verification` is `passed`, `failed`, `none` or empty, `isolation` is `managed`,
`worktree`, `docker` or `wsl`, `reproducibility` is one of the classes in [REPRODUCTION.md](REPRODUCTION.md).

## 4. Integrity verification (normative algorithm)

1. Open the archive; reject it if it is not a ZIP, exceeds a limit, or contains an unsafe entry name.
2. Read `manifest.json`; reject if `format` is not `runtide.receipt` or `formatVersion` is outside `1…supported`.
3. For every manifest file: the entry must exist, have the listed size, and its SHA-256 must match.
4. Every entry other than `manifest.json` and `signature.json` must be listed (no extra files).
5. `Integrity` is **verified** only if steps 1–4 found no problem; otherwise **failed**.

## 5. Signature

`signature.json`:

```json
{ "algorithm": "ed25519", "publicKey": "<64 hex>", "signature": "<128 hex>", "signedAt": "…" }
```

The signature is Ed25519 over `SHA-256(bytes of manifest.json)`. Because the manifest lists every other file's hash, the signature
covers the whole archive content. The signer's **fingerprint** is the first 16 hex characters of `SHA-256(publicKey)`.

Signature state is reported separately from integrity:

| State | Meaning |
| --- | --- |
| `verified` | a well-formed signature that matches the manifest. Proves only that the holder of that key signed this manifest. It additionally carries `trusted` = the fingerprint is in the reader's trusted list |
| `unavailable` | no `signature.json`: the receipt is unsigned (not an error) |
| `invalid` | `signature.json` is present but malformed, uses another algorithm, or does not match the manifest (**the receipt is then not valid**) |

A receipt is **valid** when integrity is verified and the signature state is not `invalid`.

## 6. Compatibility

* Readers accept `formatVersion` 1 up to their supported maximum and reject newer versions with an explicit message instead of
  guessing. Unknown additional JSON fields inside entries are ignored by readers.
* New optional entries or fields may be added without changing the version; a change that alters the meaning of existing fields
  increments `formatVersion`.
* Receipts written by RUNTIDE 0.2 and later use version 1 and verify with this algorithm.

## 7. What the format deliberately does not contain

Environment variable **values**, secret-looking files, absolute local paths (scrubbed to `<workspace>`, `<source>`, `<home>`),
unredacted command arguments that matched secret patterns, and any machine snapshot. Redaction is pattern based and best effort.
