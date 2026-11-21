package memtable

import (
	"LSM-Key-Value-engine/internal/enums"
	"LSM-Key-Value-engine/internal/memtable/structures"
	"LSM-Key-Value-engine/internal/record"
	"errors"
	"fmt"
	"log"
	"time"
)

type Memtable struct {
	Storage  structures.Storage
	MaxSize  uint32
	ReadOnly bool
}

var (
	ErrReadOnlyTable         = errors.New("[MEMTABLE]: This memtable is in read-only mode. Unable to edit")
	ErrTableCapacityExceeded = errors.New("[MEMTABLE]: This memtable has exceeded capacity. Flush before continuing")
)

func NewMemtable(storageKind enums.StorageKind, maxSize uint32) *Memtable {
	var storage structures.Storage

	switch storageKind {
	case enums.HashMap:
		// TODO: implement
	case enums.BTree:
		// TODO: implement
	default:
		storage = structures.NewSkipList()
	}

	return &Memtable{
		Storage:  storage,
		MaxSize:  maxSize,
		ReadOnly: false,
	}
}

func (m *Memtable) Size() uint32 {
	return m.Storage.Size()
}

func (m *Memtable) isFull() bool {
	// TODO: ask if isFull() should return simply if we already
	// TODO: exceeded MaxSize or calculate if next node can fit? or perhaps compare to a threshold?
	if m.Size() > m.MaxSize {
		return true
	}

	return false
}

func (m *Memtable) Put(key string, value []byte) error {
	if m.ReadOnly {
		return fmt.Errorf("%w", ErrReadOnlyTable)
	}
	if m.isFull() {
		return fmt.Errorf("%w. current size = %d, max size = %d", ErrTableCapacityExceeded, m.Size(), m.MaxSize)
	}

	err := m.Storage.Put(key, value)
	if err != nil {
		return err
	}

	log.Printf("[MEMTABLE]: Successfully performed 'Put' operation to key '%s'", key)
	return nil
}

func (m *Memtable) Get(key string) ([]byte, error) {
	val, err := m.Storage.Get(key)
	if err != nil {
		return nil, err
	}

	log.Printf("[MEMTABLE]: Successfully performed 'Get' operation to key '%s'", key)
	return val, nil
}

func (m *Memtable) Delete(key string) error {
	if m.ReadOnly {
		return fmt.Errorf("%w", ErrReadOnlyTable)
	}

	// since we're only deleting logically, create a new record which will replace the one we have
	deadRecord := record.Record{
		Key:       key,
		Value:     nil,
		Timestamp: uint64(time.Now().UnixMicro()),
		Tombstone: 1,
	}

	// TODO: ask about storing of value in memtable? double storing keys (once as key, once as serialized)
	err := m.Storage.Put(deadRecord.Key, deadRecord.Serialize())
	if err != nil {
		return err
	}

	log.Printf("[MEMTABLE]: Successfully performed 'Delete' operation to key '%s'", key)
	return nil
}

func (m *Memtable) Flush() error {
	// TODO: implement flushing to SSTable
	return nil
}

// Freeze sets the memtable's ReadOnly property to 'true'
func (m *Memtable) Freeze() {
	m.ReadOnly = true
}
