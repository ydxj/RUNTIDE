# Run Receipts

A **Run Receipt** (`*.runtide.zip`) is a portable, integrity-checked archive of one execution's evidence. It is *evidence*, not a
machine snapshot: it proves what was recorded, not that the result can be recreated (see [REPRODUCTION.md](REPRODUCTION.md)).

## Contents

| File | Content |
| --- | --- |
| `manifest.json` | format (`runtide.receipt`, version 1), RUNTIDE version, execution/project/task, status, verification verdict, isolation, reproducibility class, warnings, redaction summary, and **size + SHA-256 of every other file** |
| `execution.json`, `environment.json` | execution record; OS, tool versions, git commit/dirty state, **variable names only** |
| `commands.json`, `events.json` | commands (arguments redacted) and the provenance-tagged timeline |
| `processes.json`, `services.json` | processes and services observed |
| `file-changes.json` | changed paths with hashes (secret files listed as excluded, never read) |
| `verification.json` | per-check results and the overall verdict |
| `reproduction.json` | what is needed to reproduce and what is missing |
| `logs/…` | redacted output per command |
| `file-diffs.json` | only when you chose to include diffs |
| `signature.json` | only when the receipt is signed |

Absolute paths are scrubbed to `<workspace>`, `<source>` and `<home>`. Environment **values** are never included.

## Commands

```bash
runtide run export <id> -o result.runtide.zip   # create
runtide receipt verify result.runtide.zip       # integrity; exit code 3 when not valid
runtide receipt inspect result.runtide.zip      # summary: verdict, isolation, reproducibility, commands
runtide receipt report result.runtide.zip       # self-contained HTML report
runtide receipt import result.runtide.zip       # verify and index; NEVER executes anything
runtide receipt compare a.runtide.zip b.runtide.zip
runtide receipt push result.runtide.zip --server https://… --project <id>   # token in RUNTIDE_SERVER_TOKEN
```

## Verification

`receipt verify` streams the archive, checks that the manifest lists exactly the files present, that every size and SHA-256
match, that the format version is supported, and (if signed) that the signature is valid. Verification is content-addressed and
in memory: nothing is extracted to disk. Entries with unsafe names (`..`, absolute paths, odd segments) are reported as problems,
the archive is limited to 5000 entries, and each entry is read with a size cap, so decompression bombs are rejected.

## Signing

`runtide receipt keygen` creates an Ed25519 key stored under your RUNTIDE data directory. A signed receipt carries the public key
and a signature over the manifest. A valid signature proves only that **the holder of that key** produced the manifest; it says
nothing about whether you should trust that key. Trust is explicit: add the signer's fingerprint (obtained from them out of
band) under **Integrations → Trusted signer fingerprints**; otherwise the receipt shows as *signed, not trusted*.

## What a receipt cannot prove

* That the commands would behave the same elsewhere (see reproducibility classes).
* That redaction caught every secret: it is best effort. Review a receipt before sharing it.
* That the recording machine was honest: receipts are not tamper-*proof*, only tamper-*evident* against later edits (and, when
  signed and trusted, attributable).

## HTML report

`receipt report` writes one self-contained HTML file (no external requests). **PDF export is not implemented**; print the HTML
report to PDF from a browser if you need one.
