package wal

import "os"

type WAL struct {
	dir                 string // directory path for where WAL is stored, SHOULD be data/wal/
	blockSize           uint32 // from config
	segmentSizeInBlocks uint16 // from config

	segments    []string // list of all WAL segments we have, just file names
	CSN         uint64
	currSegFile *os.File // pointer to the current file, we keep it open until its filled

	blockBuf []byte
	blockOff uint32 // tells us how full the block is

	LSN uint64
	LWM uint64 // index which tells us until where we can delete WAL
}
