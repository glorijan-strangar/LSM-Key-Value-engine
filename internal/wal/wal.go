package wal

import (
	"LSM-Key-Value-engine/internal/record"
	"encoding/binary"
	"hash/crc32"
	"os"
)

const (
	typeFull   byte = 1
	typeFirst  byte = 2
	typeMiddle byte = 3
	typeLast   byte = 4

	fragCrcSize    = 4
	fragSizeSize   = 2
	fragTypeSize   = 1
	fragLogNumSize = 8

	FragHeaderSize = fragCrcSize + fragSizeSize + fragTypeSize + fragLogNumSize

	fragCrcOff     = 0
	fragSizeOff    = fragCrcSize
	fragTypeOff    = fragSizeOff + fragSizeSize
	fragLogNumOff  = fragTypeOff + fragTypeSize
	fragPayloadOff = fragLogNumOff + fragLogNumSize
)

type WAL struct {
	dir                 string // directory path for where WAL is stored, SHOULD be data/wal/
	blockSize           uint32 // from config
	segmentSizeInBlocks uint16 // from config

	segments    []string // list of all WAL segments we have, just file names
	csn         uint64
	currSegFile *os.File // pointer to the current file, we keep it open until its filled

	blockBuf []byte
	blockOff uint32 // tells us how full the block is

	lsn uint64
	lwm uint64 // index which tells us until where we can delete WAL
}

var (
	endianness = binary.LittleEndian
)

func (w *WAL) Append(rec *record.Record) {
	payload := rec.Serialize()
	canary := []byte{0xEF, 0xBE, 0xAD, 0xDE} // little endian of 0xDEADBEEF
	payload = append(payload, canary...)

	// prepare for iteration over data whilst writing
	remainingPayload := payload
	first := true

	var fragType byte
	for len(remainingPayload) > 0 {
		blockRemainingSpace := w.blockSize - w.blockOff

		if blockRemainingSpace <= FragHeaderSize { // no space for anything, add padding
			// TODO: here we would call the syncing of this block

			w.blockOff = 0
			clear(w.blockBuf)
			continue
		}

		payloadSpace := blockRemainingSpace - FragHeaderSize // the space remaining for the record inside the block
		fits := (uint32(len(remainingPayload)) <= payloadSpace)

		switch {
		case first && fits:
			fragType = typeFull
		case !first && fits:
			fragType = typeLast
		case first && !fits:
			fragType = typeFirst
		case !first && !fits:
			fragType = typeMiddle
		}

		writeSize := payloadSpace
		if fits { // if it can fit whole then thats all the payload that we are writing
			writeSize = uint32(len(remainingPayload))
		}
		fragSize := uint16(writeSize)
		fragFullSize := uint16(FragHeaderSize) + fragSize
		fragment := make([]byte, fragFullSize)

		endianness.PutUint16(fragment[fragSizeOff:], fragSize)        // size 2B
		fragment[fragTypeOff] = fragType                              // fragment type 1B
		endianness.PutUint64(fragment[fragLogNumOff:], w.lsn+1)       // log number 8B
		copy(fragment[fragPayloadOff:], remainingPayload[:writeSize]) // payload fragSize B

		fragCrc := crc32.ChecksumIEEE(fragment[fragSizeOff:])
		endianness.PutUint32(fragment[fragCrcOff:], fragCrc) // CRC 4B

		copy(w.blockBuf[w.blockOff:], fragment)
		w.blockOff += uint32(fragFullSize)

		remainingPayload = remainingPayload[writeSize:]
		first = false
	}
}
