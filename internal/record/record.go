package record

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

const (
	crcSize       = 4
	timestampSize = 8
	tombstoneSize = 1
	keySizeSize   = 8
	valueSizeSize = 8

	HeaderSize = crcSize + timestampSize + tombstoneSize + keySizeSize + valueSizeSize

	timestampOff = crcSize
	tombstoneOff = timestampOff + timestampSize
	keySizeOff   = tombstoneOff + tombstoneSize
	valueSizeOff = keySizeOff + keySizeSize
)

var (
	endianness = binary.LittleEndian

	ErrCorruptedRecord = errors.New("[RECORD]: CRC mismatch.")
	ErrInvalidRecord   = errors.New("[RECORD]: Invalid record. Size or type mismatch.")
)

type Record struct {
	Timestamp uint64 // microseconds since 1970
	Tombstone byte
	Key       []byte
	Value     []byte
}

func (r *Record) Serialize() []byte {
	totalSize := HeaderSize + len(r.Key) + len(r.Value)
	buf := make([]byte, totalSize)

	endianness.PutUint64(buf[timestampOff:], r.Timestamp)
	buf[timestampOff] = r.Tombstone
	endianness.PutUint64(buf[keySizeOff:], uint64(len(r.Key)))
	endianness.PutUint64(buf[valueSizeOff:], uint64(len(r.Value)))
	copy(buf[HeaderSize:], r.Key)
	copy(buf[HeaderSize+len(r.Key):], r.Value)

	crc := crc32.ChecksumIEEE(buf[crcSize:])
	endianness.PutUint32(buf[0:], uint32(crc))

	return buf
}

func (r *Record) Deserialize(buf []byte) (*Record, error) {
	if len(buf) < HeaderSize {
		return nil, ErrInvalidRecord
	}

	keySize := endianness.Uint64(buf[keySizeOff:])
	valueSize := endianness.Uint64(buf[valueSizeOff:])

	bufLen := uint64(len(buf))
	if keySize > bufLen {
		return nil, ErrInvalidRecord
	}
	total := uint64(HeaderSize + keySize + valueSize)
	if total > bufLen {
		return nil, ErrInvalidRecord
	}

	bufCrc := endianness.Uint32(buf[0:])
	if crc32.ChecksumIEEE(buf[crcSize:total]) != bufCrc {
		return nil, ErrCorruptedRecord
	}

	key := make([]byte, keySize)
	value := make([]byte, valueSize)
	copy(key, buf[HeaderSize:HeaderSize+keySize])
	copy(value, buf[HeaderSize+keySize:HeaderSize+keySize+valueSize])

	return &Record{
		Timestamp: endianness.Uint64(buf[timestampOff:]),
		Tombstone: buf[tombstoneOff],
		Key:       key,
		Value:     value,
	}, nil
}
