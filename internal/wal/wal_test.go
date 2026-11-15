package wal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"testing"

	"LSM-Key-Value-engine/internal/record"
)

func TestAppendWritesFullFragment(t *testing.T) {
	tests := []struct {
		name   string
		offset uint32
	}{
		{name: "at start of block", offset: 0},
		{name: "after existing data", offset: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const blockSize = 256
			const logSequenceNumber = 42

			block := bytes.Repeat([]byte{0xA5}, blockSize)
			w := &WAL{
				blockSize: blockSize,
				blockBuf:  block,
				blockOff:  tt.offset,
				lsn:       logSequenceNumber - 1,
			}
			r := &record.Record{
				Timestamp: 123456,
				Tombstone: 0,
				Key:       "key",
				Value:     []byte("value"),
			}

			payload := append(r.Serialize(), 0xEF, 0xBE, 0xAD, 0xDE)
			w.Append(r)

			fragmentStart := int(tt.offset)
			fragmentEnd := fragmentStart + FragHeaderSize + len(payload)
			fragment := block[fragmentStart:fragmentEnd]

			if got, want := binary.LittleEndian.Uint16(fragment[fragSizeOff:]), uint16(len(payload)); got != want {
				t.Errorf("fragment payload size = %d, want %d", got, want)
			}
			if got := fragment[fragTypeOff]; got != typeFull {
				t.Errorf("fragment type = %d, want full (%d)", got, typeFull)
			}
			if got, want := binary.LittleEndian.Uint64(fragment[fragLogNumOff:]), uint64(logSequenceNumber); got != want {
				t.Errorf("fragment log number = %d, want %d", got, want)
			}
			if got, want := binary.LittleEndian.Uint32(fragment[fragCrcOff:]), crc32.ChecksumIEEE(fragment[fragSizeOff:]); got != want {
				t.Errorf("fragment CRC = %#x, want %#x", got, want)
			}
			if got := fragment[fragPayloadOff:]; !bytes.Equal(got, payload) {
				t.Errorf("fragment payload = %x, want %x", got, payload)
			}
			if got, want := w.blockOff, uint32(fragmentEnd); got != want {
				t.Errorf("block offset = %d, want %d", got, want)
			}
			if tt.offset > 0 && !bytes.Equal(block[:tt.offset], bytes.Repeat([]byte{0xA5}, int(tt.offset))) {
				t.Error("append modified bytes before the starting block offset")
			}
		})
	}
}

func TestAppendFragmentThatExactlyFillsBlock(t *testing.T) {
	r := &record.Record{Timestamp: 1, Key: "k", Value: []byte("v")}
	payload := append(r.Serialize(), 0xEF, 0xBE, 0xAD, 0xDE)
	blockSize := uint32(FragHeaderSize + len(payload))
	block := make([]byte, blockSize)
	w := &WAL{blockSize: blockSize, blockBuf: block}

	w.Append(r)

	if got, want := w.blockOff, blockSize; got != want {
		t.Fatalf("block offset = %d, want %d", got, want)
	}
	if got := block[fragTypeOff]; got != typeFull {
		t.Errorf("fragment type = %d, want full (%d)", got, typeFull)
	}
	if got, want := binary.LittleEndian.Uint16(block[fragSizeOff:]), uint16(len(payload)); got != want {
		t.Errorf("fragment payload size = %d, want %d", got, want)
	}
}

func TestAppendRejectsInvalidInputAndBufferState(t *testing.T) {
	validRecord := &record.Record{Timestamp: 1, Key: "k", Value: []byte("v")}
	tests := []struct {
		name string
		wal  *WAL
		rec  *record.Record
	}{
		{
			name: "nil record",
			wal:  &WAL{blockSize: 128, blockBuf: make([]byte, 128)},
		},
		{
			name: "block too small for fragment payload",
			wal:  &WAL{blockSize: uint32(FragHeaderSize), blockBuf: make([]byte, FragHeaderSize)},
			rec:  validRecord,
		},
		{
			name: "block larger than fragment size field supports",
			wal:  &WAL{blockSize: uint32(FragHeaderSize) + uint32(^uint16(0)) + 1, blockBuf: make([]byte, uint32(FragHeaderSize)+uint32(^uint16(0))+1)},
			rec:  validRecord,
		},
		{
			name: "block buffer shorter than block size",
			wal:  &WAL{blockSize: 128, blockBuf: make([]byte, 127)},
			rec:  validRecord,
		},
		{
			name: "offset beyond block",
			wal:  &WAL{blockSize: 128, blockBuf: make([]byte, 128), blockOff: 129},
			rec:  validRecord,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.wal.Append(tt.rec); !errors.Is(err, ErrInvalidAppend) {
				t.Fatalf("Append() error = %v, want %v", err, ErrInvalidAppend)
			}
		})
	}
}

func TestDecodeFragment(t *testing.T) {
	fragmentTypes := []byte{typeFull, typeFirst, typeMiddle, typeLast}
	for _, fragmentType := range fragmentTypes {
		t.Run(map[byte]string{
			typeFull:   "full",
			typeFirst:  "first",
			typeMiddle: "middle",
			typeLast:   "last",
		}[fragmentType], func(t *testing.T) {
			payload := []byte("record bytes")
			encoded := encodeTestFragment(fragmentType, 17, payload)

			got, err := new(WAL).DecodeFragment(encoded)
			if err != nil {
				t.Fatalf("DecodeFragment() error = %v", err)
			}
			if got.Size != uint16(len(payload)) {
				t.Errorf("fragment size = %d, want %d", got.Size, len(payload))
			}
			if got.Type != fragmentType {
				t.Errorf("fragment type = %d, want %d", got.Type, fragmentType)
			}
			if got.LogNumber != 17 {
				t.Errorf("log number = %d, want 17", got.LogNumber)
			}
			if !bytes.Equal(got.Payload, payload) {
				t.Errorf("payload = %q, want %q", got.Payload, payload)
			}
		})
	}
}

func TestDecodeFragmentRejectsInvalidInput(t *testing.T) {
	valid := encodeTestFragment(typeFull, 1, []byte("payload"))

	badCRC := append([]byte(nil), valid...)
	badCRC[fragPayloadOff] ^= 0xFF

	badType := append([]byte(nil), valid...)
	badType[fragTypeOff] = 0
	writeTestFragmentCRC(badType)

	badSize := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint16(badSize[fragSizeOff:], uint16(len(valid)))

	tests := []struct {
		name  string
		input []byte
		want  error
	}{
		{name: "short header", input: make([]byte, FragHeaderSize-1), want: ErrInvalidFragment},
		{name: "truncated payload", input: valid[:len(valid)-1], want: ErrInvalidFragment},
		{name: "declared size exceeds input", input: badSize, want: ErrInvalidFragment},
		{name: "bad CRC", input: badCRC, want: ErrCorruptedFragment},
		{name: "unknown type", input: badType, want: ErrInvalidFragment},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := new(WAL).DecodeFragment(tt.input); !errors.Is(err, tt.want) {
				t.Fatalf("DecodeFragment() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func encodeTestFragment(fragmentType byte, logNumber uint64, payload []byte) []byte {
	fragment := make([]byte, FragHeaderSize+len(payload))
	binary.LittleEndian.PutUint16(fragment[fragSizeOff:], uint16(len(payload)))
	fragment[fragTypeOff] = fragmentType
	binary.LittleEndian.PutUint64(fragment[fragLogNumOff:], logNumber)
	copy(fragment[fragPayloadOff:], payload)
	writeTestFragmentCRC(fragment)
	return fragment
}

func writeTestFragmentCRC(fragment []byte) {
	binary.LittleEndian.PutUint32(fragment[fragCrcOff:], crc32.ChecksumIEEE(fragment[fragSizeOff:]))
}
