package record

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
)

func TestSerializeDeserializeRoundTrip(t *testing.T) {
	original := &Record{
		Timestamp: 1_735_689_600_123_456,
		Tombstone: 0,
		Key:       "user:42",
		Value:     []byte("Ada Lovelace"),
	}

	encoded := original.Serialize()
	if got, want := len(encoded), HeaderSize+len(original.Key)+len(original.Value); got != want {
		t.Fatalf("serialized size = %d, want %d", got, want)
	}

	decoded, err := Deserialize(encoded)
	if err != nil {
		t.Fatalf("Deserialize() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("round trip = %#v, want %#v", decoded, original)
	}
}

func TestSerializeUsesCRCFreeLittleEndianLayout(t *testing.T) {
	r := &Record{Timestamp: 0x0102030405060708, Tombstone: 1, Key: "k", Value: []byte("v")}
	encoded := r.Serialize()

	if got, want := HeaderSize, 25; got != want {
		t.Fatalf("HeaderSize = %d, want %d", got, want)
	}
	if got, want := len(encoded), HeaderSize+2; got != want {
		t.Fatalf("serialized length = %d, want %d", got, want)
	}
	if got, want := binary.LittleEndian.Uint64(encoded[0:8]), r.Timestamp; got != want {
		t.Fatalf("timestamp = %#x, want %#x", got, want)
	}
	if encoded[8] != r.Tombstone {
		t.Fatalf("tombstone byte = %d, want %d", encoded[8], r.Tombstone)
	}
	if got := binary.LittleEndian.Uint64(encoded[9:17]); got != 1 {
		t.Fatalf("key length = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint64(encoded[17:25]); got != 1 {
		t.Fatalf("value length = %d, want 1", got)
	}
	if string(encoded[HeaderSize:]) != "kv" {
		t.Fatalf("payload = %q, want %q", encoded[HeaderSize:], "kv")
	}
}

func TestSerializeDeserializeEmptyValue(t *testing.T) {
	original := &Record{Timestamp: 9, Key: "empty", Value: []byte{}}

	decoded, err := Deserialize(original.Serialize())
	if err != nil {
		t.Fatalf("Deserialize() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("round trip = %#v, want %#v", decoded, original)
	}
}

func TestSerializeDeserializeTombstone(t *testing.T) {
	original := &Record{Timestamp: 10, Tombstone: 1, Key: "deleted", Value: []byte{}}

	decoded, err := Deserialize(original.Serialize())
	if err != nil {
		t.Fatalf("Deserialize() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("round trip = %#v, want %#v", decoded, original)
	}
}

func TestDeserializeRejectsInvalidSizes(t *testing.T) {
	valid := (&Record{Timestamp: 1, Key: "k", Value: []byte("v")}).Serialize()

	tests := []struct {
		name  string
		input []byte
	}{
		{name: "short header", input: make([]byte, HeaderSize-1)},
		{name: "truncated payload", input: valid[:len(valid)-1]},
		{name: "declared payload exceeds input", input: valid[:HeaderSize]},
	}

	badKeySize := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint64(badKeySize[keySizeOff:], uint64(len(valid)+1))
	tests = append(tests, struct {
		name  string
		input []byte
	}{name: "key size exceeds input", input: badKeySize})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Deserialize(tt.input)
			if !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("Deserialize() error = %v, want %v", err, ErrInvalidRecord)
			}
		})
	}
}
