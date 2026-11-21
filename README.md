# kvengine

A course project to build a persistent key-value storage engine in Go. The design uses a write-ahead log, in-memory memtables, and immutable sorted files on disk (SSTables), with reads served through memtables, a cache, and SSTables. The console entry point is currently a stub.

> Course project (Advanced Algorithms and Data Structures, 2026/2027). Current scope: everything required for grade 8. Compaction and the LSM tree are out of scope for now (see [Roadmap](#roadmap)).

## Table of contents

1. [Features](#features)
2. [Quick start](#quick-start)
3. [Project layout](#project-layout)
4. [Architecture](#architecture)
5. [Record and WAL format](#record-and-wal-format)
6. [Components](#components)
7. [On-disk layout](#on-disk-layout)
8. [Configuration](#configuration)
9. [Console menu](#console-menu)
10. [Testing](#testing)
11. [Status](#status)
12. [Open design decisions](#open-design-decisions)
13. [Roadmap](#roadmap)

## Features

These are the planned engine features. See [Status](#status) for implementation progress.

- `PUT(key, value)`, `GET(key)`, `DELETE(key)` backed by a real write path and read path
- Segmented, block-based write-ahead log with CRC checks, fragmentation of large records, padding, trailing canary and `fdatasync`
- Memtable with pluggable implementation (hash map or skip list), optionally split into `N` instances
- SSTables made of Data, Filter (Bloom), Index, Summary (configurable sparsity) and Metadata (Merkle tree)
- Merkle-tree validation of any SSTable, started by the user
- LRU read cache and LRU block cache
- All file I/O goes through a block manager (4 KB, 8 KB or 16 KB blocks)
- Token-bucket rate limiting
- All settings come from an external JSON file, with defaults in code for anything missing

## Quick start

The Go version is declared in `go.mod` (currently 1.27).

```bash
git clone https://github.com/glorijan-strangar/LSM-Key-Value-engine.git
cd LSM-Key-Value-engine
```

The console executable and its command line options are planned. See [Testing](#testing) for the repository's test commands.

## Project layout

The tree includes planned packages and directories alongside the current source.

```
LSM-Key-Value-engine/
├── cmd/
│   └── kvengine/
│       └── main.go              # console entry point (stub)
├── internal/
│   ├── enums/                   # StorageKind for selecting a memtable backend
│   ├── record/                  # Record type and binary serialization
│   ├── lru/                     # generic LRU cache (used by block cache and read cache)
│   ├── blockmanager/            # block read/write by (path, block number) + block cache
│   ├── wal/                     # segmented write-ahead log, recovery
│   │   ├── sync_linux.go        #   fdatasync (build tag: linux)
│   │   └── sync_other.go        #   Sync() fallback for other platforms
│   ├── memtable/                # Memtable operations and storage selection
│   │   └── structures/          # Storage interface and skip_list.go
│   ├── bloom/                   # Bloom filter
│   ├── merkle/                  # Merkle tree build + validation
│   ├── sstable/                 # writer, reader, data, index, summary, filter, metadata
│   ├── ratelimit/               # token bucket
│   ├── engine/                  # orchestrates write path and read path
│   └── cli/                     # interactive console menu
├── configs/
│   ├── config.go                # JSON loading
│   ├── defaults.go              # default values
│   └── config.json              # configuration file placeholder
├── docs/                        # diagrams, design notes
├── data/                        # runtime data (gitignored)
├── go.mod
└── README.md
```

Each package maps to one component of the specification. Dependencies only point in one direction, so every layer can be developed and tested on its own:

```mermaid
flowchart TD
    cli --> engine
    engine --> ratelimit
    engine --> wal
    engine --> memtable
    engine --> sstable
    engine --> lru
    sstable --> bloom
    sstable --> merkle
    sstable --> blockmanager
    wal --> blockmanager
    blockmanager --> lru
    wal --> record
    sstable --> record
    memtable --> record
    memtable --> structures
    memtable --> enums
    engine --> configs
```

`internal/` keeps everything private to the module. `cmd/` stays thin. `engine` knows nothing about the console, so it can be tested without it.

## Architecture

These diagrams describe the intended engine once its components are connected.

### Write path

```mermaid
flowchart LR
    U["PUT / DELETE"] --> RL["Rate limiter"]
    RL --> W["WAL append + fdatasync"]
    W --> M["Active memtable"]
    M -->|"all N tables full"| F["Flush (sorted by key)"]
    F --> S["New SSTable on disk"]
    F --> D["Delete covered WAL segments"]
```

1. The request passes the token bucket.
2. The operation is appended to the WAL and synced to disk.
3. Only after the WAL confirms the write, it is applied to the active memtable. A `DELETE` writes a record with the tombstone flag set.
4. With `N` memtables, one is read-write and `N-1` are read-only. When the active one is full it becomes read-only and a fresh one takes over. When all `N` are full, they are flushed to a new SSTable.
5. WAL segments are removed only when everything they contain is persisted in an SSTable.

### Read path

```mermaid
flowchart LR
    G["GET key"] --> M["Memtables (newest first)"]
    M -->|miss| C["Read cache (LRU)"]
    C -->|miss| T["SSTables (newest first)"]
    T --> B["Bloom filter"]
    B -->|"maybe"| S["Summary range check"]
    S --> I["Index"]
    I --> D["Data"]
```

1. Memtables, newest to oldest. A hit, including a tombstone, ends the search.
2. Read cache. A hit ends the search.
3. SSTables, newest to oldest. For each one: Bloom filter, then Summary range (min/max key), then Index, then Data. A tombstone means "not found".
4. A value found on disk is put into the read cache. `PUT` and `DELETE` update or invalidate the cached entry so the cache never serves stale data.

## Record and WAL format

The same logical record is intended for the WAL and SSTable Data. `(*Record).Serialize()` encodes a record and the package function `record.Deserialize(buf)` decodes it.

```
+----------------+----------------+--------------+-----------------+-----+-------+
| Timestamp (8B) | Tombstone (1B) | Key Size (8B) | Value Size (8B) | Key | Value |
+----------------+----------------+--------------+-----------------+-----+-------+
```

| Field | Offset | Size | Description |
|---|---|---|---|
| Timestamp | 0 | 8 B | Time of the operation in Unix microseconds |
| Tombstone | 8 | 1 B | `0` = live record, `1` = deleted. A deleted record has Value Size `0` and no value bytes |
| Key Size | 9 | 8 B | Length of Key in bytes (`uint64`) |
| Value Size | 17 | 8 B | Length of Value in bytes (`uint64`) |
| Key | 25 | Key Size | Raw bytes of the Go string |
| Value | 25 + Key Size | Value Size | Raw bytes. The console accepts values as strings and converts them to bytes |

- All numeric fields are **little-endian**.
- The fixed header is $8 + 1 + 8 + 8 = 25$ bytes.
- CRC checks belong to the WAL fragment layer. The record layout has no CRC field.
- Serialization uses only `encoding/binary`. No serialization libraries are used.

See [Write-ahead log](#write-ahead-log-wal) for the WAL fragment layout.

## Components

### Block manager and block cache

All reads and writes of files go through the block manager, never directly through `os.File`.

- The disk is divided into fixed-size blocks (4 KB, 8 KB or 16 KB, set in the config).
- Read: `(path, blockNumber)`. The block cache is checked first, and only on a miss is the block read from disk and added to the cache.
- Write: `(path, blockNumber, content)`. The block is written to disk, and if it is in the cache, the cached copy is updated too.
- The block cache uses LRU eviction and its size is configurable.
- A reader/writer wrapper on top of the block manager lets higher layers treat a file as a byte stream, since a record can cross block boundaries.

### Write-ahead log (WAL)

The WAL package is absent from the current memtable branch. The format below documents the design.

- Append-only, split into segments of a fixed size given in blocks.
- A record larger than the free space in the current block is fragmented across blocks. Padding is applied where a block cannot be filled.
- Each record ends with a trailing canary field.
- Content is flushed to disk with `fdatasync` (`syscall.Fdatasync` on Linux, `File.Sync()` elsewhere, selected by build tags).

Each WAL fragment has a 15-byte header followed by its payload:

```
+-----------+--------------+--------------+----------------+---------+
| CRC (4 B) | Size (2 B)   | Type (1 B)   | Log number (8 B)| Payload |
+-----------+--------------+--------------+----------------+---------+
```

| Field | Offset | Size | Description |
|---|---:|---:|---|
| CRC | 0 | 4 B | CRC32 (IEEE) over the bytes from Size through the end of this fragment's Payload |
| Size | 4 | 2 B | Payload length in bytes (`uint16`) |
| Type | 6 | 1 B | `1` = FULL, `2` = FIRST, `3` = MIDDLE, `4` = LAST |
| Log number | 7 | 8 B | Sequence number identifying the logical WAL record (`uint64`) |
| Payload | 15 | Size | Part or all of the serialized record; a complete record payload ends with the 4-byte canary `EF BE AD DE` |

All numeric fields are little-endian. A FULL fragment contains one complete serialized record and its canary. A fragmented record is represented by FIRST, zero or more MIDDLE fragments, and LAST; the fragments share a log number. Block padding is outside fragment payloads and is not covered by a fragment CRC.

`DecodeFragment` accepts exactly one encoded fragment, from its CRC byte through the last payload byte. The input must exclude block padding and any following fragments. It checks the fragment size and CRC before returning its payload.

- Records are read one by one, never by loading a whole segment into memory. The CRC is checked on every fragment.
- On startup the WAL is replayed in order to rebuild the memtable(s).
- A segment is deleted only after all of its data is persisted in an SSTable.

### Memtable

`NewMemtable` takes an `enums.StorageKind` and a size limit. `SkipList` is the implemented backend. The enum also declares `HashMap` and `BTree` for future backends. Backend selection is currently passed to the constructor in code.

The `structures.Storage` interface exposes `Get`, `Put`, `Delete`, and `Size`. The skip list stores values at the bottom level and supports physical removal. At the memtable layer, `Delete` instead upserts a serialized tombstone with an empty value and a Unix microsecond timestamp. `Get` returns the stored bytes. `Freeze` makes the memtable reject subsequent `Put` and `Delete` calls.

The skip list's size accounting and the memtable's capacity policy are still in progress. Sorted iteration, SSTable flushing, and rotation across `N` memtables are planned. The skip list keeps keys in order, and the planned hash map backend will sort its keys for a flush.

### SSTable

An SSTable is a directory of files:

| File | Content |
|---|---|
| Data | Records sorted by key. Never loaded whole, read block by block |
| Filter | Bloom filter over all keys in Data |
| Index | Key and offset into Data for every record. Read block by block |
| Summary | Min key and max key of the Index, plus every $k$-th Index entry (key and offset into Index). $k$ is the configurable sparsity. Read block by block |
| Metadata | Merkle tree over all values in Data |

**Lookup:** Bloom filter says "maybe". Check that the key is within the Summary range. Find the Index position from Summary. Find the Data offset in the Index. Read the record.

**Bloom filter sizing.** For $n$ expected keys and false-positive rate $p$, the number of bits $m$ and hash functions $k$ are

$$m = -\frac{n \ln p}{(\ln 2)^2}, \qquad k = \frac{m}{n}\ln 2 .$$

**Merkle validation.** The user selects an SSTable. The engine rebuilds the tree from the current Data file and compares it with the stored one, reporting whether and where the data was changed.

### Read cache

LRU cache of `key -> record` with a configurable maximum size. It sits between the memtables and the SSTables and is updated on every `PUT` and `DELETE`.

### Rate limiting

Token bucket with configurable token count and refill interval. Its state is serialized and stored in the engine like any other record, under a reserved key. The user cannot read, write or delete it through the normal operations.

## On-disk layout

```
data/
├── wal/
│   ├── wal_000001.log
│   └── wal_000002.log
└── sstable/
    ├── sst_L0_000001/
    │   ├── data.db
    │   ├── index.db
    │   ├── summary.db
    │   ├── filter.db
    │   └── metadata.db
    └── sst_L0_000002/
```

The `L0` in the name reserves the level for the future LSM tree, so files will not need renaming later.

## Configuration

`configs.LoadConfig()` reads `config.json` from the working directory. It starts with the defaults below and overrides fields present in the JSON file. A missing or empty file uses the defaults. The file under `configs/` is currently a placeholder.

```json
{
  "block_size_kb": 4,
  "block_cache_size_kb": 4096,
  "cache_size_kb": 2048,
  "summary_sparsity": 4,
  "wal_segment_size_in_blocks": 64,
  "memtable_max_size_kb": 2048,
  "memtable_instances_count": 2,
  "token_bucket_capacity": 100,
  "token_bucket_refill_interval_ms": 1000
}
```

| Key | Meaning |
|---|---|
| `block_size_kb` | Block size in KB |
| `block_cache_size_kb` | Block cache capacity in KB |
| `cache_size_kb` | Read cache capacity in KB |
| `summary_sparsity` | Every $k$-th Index entry goes to Summary |
| `wal_segment_size_in_blocks` | WAL segment size in blocks |
| `memtable_max_size_kb` | Memtable size limit in KB |
| `memtable_instances_count` | Number of memtables `N` |
| `token_bucket_capacity` | Bucket size in tokens |
| `token_bucket_refill_interval_ms` | Refill interval in milliseconds |

The loader reports file read and JSON decoding errors. Range validation, backend selection from JSON, and a memtable limit by element count are pending.

## Console menu

Planned menu:

```
1) PUT
2) GET
3) DELETE
4) Validate SSTable (Merkle tree)
5) Exit
```

Keys are strings. Values are entered as strings and converted to bytes by the engine.

## Testing

```bash
go vet ./...
go test ./...
```

The existing record tests cover the binary layout, round trips, empty values, tombstones, and malformed or truncated input. Planned coverage for the remaining components includes:

- WAL: many records over several segments, fragmented records, a damaged tail, replay after a restart
- block manager: cache hit on the second read, cache update on write
- SSTable: lookup through Summary, Index and Data, missing keys, tombstones
- engine: `PUT`, restart, `GET`, including after a flush

## Development workflow & conventions

To keep the codebase clean and maintainable, please follow these branching, commit, and coding conventions while working on the project.

### 1. Branching strategy
We use feature and fix branches branching off from `main`. Keep pull requests scoped to a single logical change.

* **Feature branches:** `feature/<short-description>` (e.g., `feature/skiplist-memtable`, `feature/bloom-filter`)
* **Bug fixes:** `fix/<short-description>` (e.g., `fix/wal-crc-checksum`, `fix/sstable-reader-offset`)

Never push directly to `main`. Open a pull request once your implementation and local tests are ready.

### 2. Commit conventions
* **How often to commit:** Commit early and often. Make a commit whenever you complete a logical, working, and tested step (e.g., after writing a specific function, fixing a bug, or adding a unit test). Avoid large, monolithic commits that bundle unrelated changes.
* **Rule of thumb:** One commit should represent a single, self-contained change that leaves the codebase in a stable state. Never commit broken code or failing tests to a feature branch unless it is explicitly marked as a temporary Work In Progress (`WIP`).

We follow conventional commit guidelines to keep the Git history readable and structured. Each commit message must follow this format:

~~~~text
<type>(<scope>): <short summary>
~~~~

**Allowed types:**
* `feat`: Adding a new feature or component (e.g., `feat(wal): implement block-based segment rotation`)
* `fix`: Fixing a bug (e.g., `fix(blockmanager): handle out-of-bounds block index read`)
* `refactor`: Code change that neither fixes a bug nor adds a feature (e.g., `refactor(sstable): clean up summary index search logic`)
* `test`: Adding or updating tests (e.g., `test(memtable): add concurrent skip list unit tests`)
* `docs`: Documentation updates (e.g., `docs(readme): update project layout tree`)
* `chore`: Maintenance tasks, dependencies, or tooling (e.g., `chore: update go version`)

**Examples of good commits:**
~~~~bash
git commit -m "feat(merkle): implement tree generation for sstable metadata"
git commit -m "fix(engine): resolve get path race condition on memtable flush"
~~~~

### 4. Issue tracking & management
We use GitHub Issues to track bugs, plan upcoming features, and discuss design decisions. Keeping issues organized helps track progress across components.

* **When to open an issue:**
  * **Bug Reports:** When you encounter unexpected behavior, data corruption, or panic during testing or normal execution.
  * **Feature Proposals:** Before starting a complex new module (e.g., adding compaction or LSM tree layers), open an issue to outline the design.
  * **Task Breakdown:** For splitting larger coursework requirements into manageable sub-tasks.

* **How to write a good issue:**
  * **Clear Title:** Use a descriptive title prefixed with the component or scope (e.g., `[WAL] Segment rotation fails when block boundary is exactly reached` or `[SSTable] Design Merkle tree validation flow`).
  * **Description/Context:** Provide steps to reproduce for bugs, or a clear specification/proposal for new features.
  * **Linking PRs:** Reference the issue number in your pull request description (e.g., `Closes #12` or `Relates to #8`) so they automatically link and close upon merging.

### 4. Go coding conventions & project standards
* **Error handling:** Never ignore errors using `_`. Always handle them explicitly, wrap them with context using `fmt.Errorf("...: %w", err)` where appropriate, and return them up the stack.
* **File I/O & Block alignment:** All file operations must go through the block manager using fixed-size blocks (4KB, 8KB, or 16KB). Avoid direct unbuffered reads/writes outside designated components.
* **Concurrency:** Ensure components shared across read and write paths (such as the memtable, cache, or block manager) are thread-safe and properly guarded using sync primitives (`sync.RWMutex`, `sync.Mutex`).
* **Configuration:** Do not hardcode magic numbers, paths, or limits. Fetch settings dynamically from the loaded configuration struct, falling back to safe defaults if fields are missing.
* **Testing:** Every new module or significant change should include corresponding unit tests under the same package or in a `_test.go` file. Run tests locally before opening a PR:
  ~~~~bash
  go test ./... -v
  ~~~~

## Status

**Grade 6**
- [ ] Config (2.1)
- [x] Record
- [ ] Block manager and block cache (1.6)
- [ ] WAL (1.1)
- [ ] Memtable, hash map (1.2)
- [ ] SSTable: Data, Filter, Index, Summary (1.3, without 1.3.5)
- [ ] Engine write path and read path, console menu

**Grade 7**
- [ ] Merkle tree metadata and validation (1.3.5)
- [ ] LRU read cache (1.5)
- [ ] Skip list memtable (1.2[DZ1]) — basic storage and memtable operations are in place
- [ ] Configurable Summary sparsity (1.3[DZ1])

**Grade 8**
- [ ] Token bucket (2.2)
- [ ] WAL with block-sized segments, fragmentation, padding, canary, `fdatasync` (1.1[DZ1])
- [ ] `N` memtables (1.2[DZ2])

## Open design decisions

These follow the course material and should be confirmed:

- Whether a fragmented record may cross a segment boundary.
- Whether SSTable components stay as separate files (current choice) or become one file.
- Hash function for the Merkle tree.
- With `N` memtables, whether all of them are flushed together into one SSTable (current choice) or one at a time.

## Roadmap

Beyond the current scope: LSM tree with size-tiered and leveled compaction, snapshots and checkpoints, prefix and range scans, iterators, SSTable compression (variable-length and delta encoding), data block hash index, MANIFEST and recovery, backups, probabilistic types (Bloom filter, Count-Min Sketch, HyperLogLog, SimHash), batch operations, TTL, BLOBs and keyspaces.
