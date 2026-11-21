package enums

type StorageKind uint8

const (
	SkipList StorageKind = iota
	HashMap
	BTree
)

func (sk StorageKind) String() string {
	return [...]string{"SkipList", "HashMap", "BTree"}[sk]
}
