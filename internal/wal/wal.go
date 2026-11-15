package wal

import (
	"LSM-Key-Value-engine/internal/record"
	"encoding/binary"
	"errors"
	"fmt"
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
	dir                 string // directory path for where WAL segments are stored, SHOULD be data/wal/
	blockSize           uint32 // from config
	segmentSizeInBlocks uint16 // from config

	segments    []string // list of all WAL segments we have, just file names
	csn         uint64   // current segment number
	currSegFile *os.File // pointer to the current file, we keep it open until its filled

	blockBuf []byte
	blockOff uint32 // tells us how full the block is

	lsn uint64
	lwm uint64 // index which tells us until where we can delete WAL
}

type Fragment struct {
	Size      uint16
	Type      byte
	LogNumber uint64
	Payload   []byte
}

var (
	endianness = binary.LittleEndian

	ErrCorruptedFragment = errors.New("[WAL]: Encountered corrupted fragament. CRC doesn't match.")
	ErrInvalidFragment   = errors.New("[WAL]: Invalid fragment layout or unkown value spotted.")
	ErrInvalidAppend     = errors.New("[WAL]: Invalid append input or buffer state.")
)

// Append encodes rec with a trailing canary and appends its WAL fragment or
// fragments to the current in-memory block buffer. It returns an error if the
// record is nil or the configured block buffer is invalid.
func (w *WAL) Append(rec *record.Record) error {
	if rec == nil {
		return fmt.Errorf("%w: record is nil", ErrInvalidAppend)
	}
	if w.blockSize <= FragHeaderSize {
		return fmt.Errorf("%w: block size %d must exceed fragment header size %d", ErrInvalidAppend, w.blockSize, FragHeaderSize)
	}
	if w.blockSize > FragHeaderSize+uint32(^uint16(0)) {
		return fmt.Errorf("%w: block size %d exceeds maximum representable fragment size", ErrInvalidAppend, w.blockSize)
	}
	if uint64(len(w.blockBuf)) < uint64(w.blockSize) {
		return fmt.Errorf("%w: block buffer length %d is smaller than block size %d", ErrInvalidAppend, len(w.blockBuf), w.blockSize)
	}
	if w.blockOff > w.blockSize {
		return fmt.Errorf("%w: block offset %d exceeds block size %d", ErrInvalidAppend, w.blockOff, w.blockSize)
	}

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

	return nil
}

// DecodeFragment decodes exactly one fragment, including its header and
// payload. frag must exclude block padding and any following fragments. The
// returned payload references frag's backing array.
func (w *WAL) DecodeFragment(frag []byte) (*Fragment, error) {
	if len(frag) < FragHeaderSize {
		return nil, fmt.Errorf("%w: frag length = %d, fragHeaderSize = %d", ErrInvalidFragment, len(frag), FragHeaderSize)
	}

	fragSize := endianness.Uint16(frag[fragSizeOff : fragSizeOff+fragSizeSize])

	if int(fragSize) != len(frag)-FragHeaderSize {
		return nil, fmt.Errorf("%w: fragSize = %d, frag length - FragHeaderSize = %d", ErrInvalidFragment, fragSize, len(frag)-FragHeaderSize)
	}

	fragCrc := endianness.Uint32(frag[fragCrcOff : fragCrcOff+fragCrcSize])
	// crc calculated on the spot
	crc := crc32.ChecksumIEEE(frag[fragSizeOff : FragHeaderSize+int(fragSize)])

	if crc != fragCrc {
		return nil, fmt.Errorf("%w: fragCrc = %x, crc = %x", ErrCorruptedFragment, fragCrc, crc)
	}

	fragType := frag[fragTypeOff]

	if fragType < byte(1) || fragType > byte(4) {
		return nil, fmt.Errorf("%w: fragType = %d", ErrInvalidFragment, fragType)
	}
	fragLogNum := endianness.Uint64(frag[fragLogNumOff : fragLogNumOff+fragLogNumSize])
	fragPayload := frag[fragPayloadOff : fragPayloadOff+int(fragSize)]

	return &Fragment{
		fragSize,
		fragType,
		fragLogNum,
		fragPayload,
	}, nil
}
