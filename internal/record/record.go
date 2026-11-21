package record

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	timestampSize = 8
	tombstoneSize = 1
	keySizeSize   = 8
	valueSizeSize = 8

	HeaderSize = timestampSize + tombstoneSize + keySizeSize + valueSizeSize

	timestampOff = 0
	tombstoneOff = timestampOff + timestampSize
	keySizeOff   = tombstoneOff + tombstoneSize
	valueSizeOff = keySizeOff + keySizeSize
)

var (
	endianness = binary.LittleEndian

	ErrInvalidRecord = errors.New("[RECORD]: Invalid record. Size or type mismatch")
)

type Record struct {
	Timestamp uint64 // microseconds since 1970
	Tombstone byte
	Key       string
	Value     []byte
}

func (r *Record) Serialize() []byte {
	totalSize := HeaderSize + len(r.Key) + len(r.Value)
	buf := make([]byte, totalSize)

	endianness.PutUint64(buf[timestampOff:], r.Timestamp)
	buf[tombstoneOff] = r.Tombstone
	endianness.PutUint64(buf[keySizeOff:], uint64(len(r.Key)))
	endianness.PutUint64(buf[valueSizeOff:], uint64(len(r.Value)))
	copy(buf[HeaderSize:], r.Key)
	copy(buf[HeaderSize+len(r.Key):], r.Value)

	return buf
}

func Deserialize(buf []byte) (*Record, error) {
	if len(buf) < HeaderSize {
		return nil, ErrInvalidRecord
	}

	keySize := endianness.Uint64(buf[keySizeOff:])
	valueSize := endianness.Uint64(buf[valueSizeOff:])

	bufLen := uint64(len(buf))
	if keySize > bufLen {
		return nil, fmt.Errorf("%w. keySize = %d, bufLen = %d", ErrInvalidRecord, keySize, bufLen)
	}
	total := uint64(HeaderSize + keySize + valueSize)
	if total > bufLen {
		return nil, fmt.Errorf("%w. total = %d, bufLen = %d", ErrInvalidRecord, total, bufLen)
	}

	key := string(buf[HeaderSize : HeaderSize+keySize])
	value := make([]byte, valueSize)
	copy(value, buf[HeaderSize+keySize:HeaderSize+keySize+valueSize])

	return &Record{
		Timestamp: endianness.Uint64(buf[timestampOff:]),
		Tombstone: buf[tombstoneOff],
		Key:       key,
		Value:     value,
	}, nil
}
