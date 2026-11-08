# Per-buffer hot-exit store (decision)

R2-5 asks for a per-buffer hot-exit store and file-launch unsaved-text restore. This is the
recorded design and the reason it is deferred; the landed model stays the one-session-file
model of `session/` + `internal/app` (session.json with a stashed-text file per unsaved
document).

## Problem being closed

With one session file, two things cannot work:

- **file-launch restore**: `gvim file.go` from a shell cannot see the unsaved text other files
  had when the last session ended, because that text lives in that session's stash;
- **`--no-restore`**: the flag skips opening the session but the editor still saves at exit
  (§4 item 10), so the session must keep its stashes even when the user asked not to restore.

## Design sketch (if scheduled)

- **Location and naming**: a side store under `<config.Dir()>/buffers/`, one entry per
  canonical path: `<sha256(canonical path)>.json` plus side text files, written through
  `atomicfile`. It never lives in the workspace (a repository must not own unsaved text).
- **Entry format** (versioned, JSON): `{version, path, size, mtimeUnixNano, savedAt,
  sessionID, stash}` where `stash` is the same stash content the session model already
  writes (inline text for small buffers, the document overlay delta for large files), so the
  restore path reuses `session.StashContent`/`StashOverlay` and their stat policy.
- **Write rules**: on session save (exit, Reload, daemon detach) every unsaved buffer gets an
  entry, independent of which files the launching command line will restore. A clean buffer's
  entry is deleted. The store is flushed after the session file so a crash leaves the session
  authoritative.
- **Read rules**: at file open, when hot exit is on and the session restore did not already put
  text in the tab, the store's entry for the canonical path is offered through the same
  "restore unsaved text" path as a stash (warn on a content mismatch, never overwrite the file
  unasked). `--no-restore` still writes the store at exit; it just skips reading.
- **Bounds and GC**: a cap on entry count and total bytes (LRU by `savedAt`; e.g. 512 entries /
  64 MiB, both config-visible), entries older than a retention window (e.g. 30 days) pruned at
  startup, entries whose file disappeared with no session reference pruned, and explicit
  deletes on clean save/close.
- **Ownership**: one writer at a time (the process that owns the session lock; daemon attach
  transfers it) — the store write is part of the same exit/detach sequence as the session
  save, so the daemon single-writer refusal covers it for free.
- **Trust and ephemera**: restore is gated by the open-time trust state (untrusted workspace:
  offer, do not silently apply), respects ephemeral-file patterns, and unknown versions are
  ignored (forward-compatible: old sessions/stores read unchanged).

## Why deferred (M/L, not half-landed)

The one-session-file model is load-bearing for restore order, the quit prompt, hot-exit policy
and the daemon's attach/detach ownership. A bounded store is a coherent next slice, but a
partial one would create two writers of unsaved text (the session stashes and the side store)
with no single GC point; it also needs the running session lock, the daemon single-writer
refusal, the launch-time open path and the `--no-restore` interplay scheduled together. Until
then the session file keeps the stashed text, file-launch restore of other files' unsaved text
is not offered, and `--no-restore` remains deliberately "skip reading, still save" (§4 item 10).

Tracked as R2-5; the format sketch above is the precise scope to pick up.
