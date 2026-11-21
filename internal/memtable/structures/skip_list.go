package structures

import (
	"errors"
	"fmt"
	"math/rand"
)

type skipListNode struct {
	RightPtr *skipListNode
	DownPtr  *skipListNode

	Key string
	Val []byte // null if not bottom node, dont need to store at all levels
}

func newSkipListNode(key string, val []byte) *skipListNode {
	return &skipListNode{
		RightPtr: nil, // set to null at the beginning
		DownPtr:  nil,
		Key:      key,
		Val:      val,
	}
}

const (
	MAX_HEIGHT = 32
)

type SkipList struct {
	head       *skipListNode
	currHeight uint8
	size       uint32 // gives us max cap at around 4GB, nobody is making a 4GB memtable
}

var (
	ErrKeyNotFound = errors.New("[SKIP_LIST]: Key not found in list")
)

func NewSkipList() *SkipList {
	return &SkipList{
		head:       newSkipListNode("", nil), // for now set to nil
		currHeight: 1,
		size:       0,
	}
}

func coinFlipHeads() bool {
	if rand.Intn(2) == 0 {
		return true
	}

	return false
}

func (sl *SkipList) Size() uint32 {
	return sl.size
}

// search returns the predecessor of key at every active level and the matching
// bottom-level node if present. predecessors[0] belongs to the bottom level
// the last entry belongs to the top level. each level must start with a head
// and sl.head must point to the top-left node
func (sl *SkipList) search(key string) (predecessors []*skipListNode, found *skipListNode) {
	if sl.head == nil {
		return nil, nil
	}

	predecessors = make([]*skipListNode, sl.currHeight)
	currNode := sl.head

	// iterate in reverse order, so predecessors are stored bottom to top
	// eg. predecessors[0] is the bottom level...
	for level := int(sl.currHeight) - 1; level >= 0; level-- {
		// Stop BEFORE an equal key, this way we record the insert position even
		// when the key is absent
		for currNode.RightPtr != nil && currNode.RightPtr.Key < key {
			currNode = currNode.RightPtr
		}

		// save this levels predecessor before descending. set needs these
		// nodes because the right/down links cannot take it back up later
		predecessors[level] = currNode
		currNode = currNode.DownPtr
	}

	// vals exist only at the bottom, so finish the traversal even if an
	// upper-level copy of the key was encountered.
	candidate := predecessors[0].RightPtr
	if candidate != nil && candidate.Key == key {
		found = candidate
	}
	return predecessors, found
}

func (sl *SkipList) Get(targetKey string) ([]byte, error) {
	_, found := sl.search(targetKey) // we dont care about predecessors for the Get function

	if found != nil {
		return found.Val, nil
	}

	return nil, fmt.Errorf("%w. targetKey = %s", ErrKeyNotFound, targetKey)
}

func (sl *SkipList) Put(key string, val []byte) error {
	// TODO: update size
	// calculate number of insertions
	insHeight := uint8(1) // 1 is the lowest because we have to add to the first row
	for {
		heads := coinFlipHeads()
		if heads && insHeight < MAX_HEIGHT {
			insHeight++
			continue
		}

		break
	}

	predecessors, found := sl.search(key)
	// if we found an element, we can just swap out the vall and call it a day
	if found != nil {
		found.Val = val
		return nil
	}

	var below *skipListNode
	for level := uint8(0); level < insHeight; level++ {
		// add the base head node
		if level >= sl.currHeight {
			newHead := newSkipListNode("", nil)
			newHead.DownPtr = sl.head
			sl.head = newHead

			predecessors = append(predecessors, sl.head)
			sl.currHeight++
		}

		node := newSkipListNode(key, nil)
		if level == 0 {
			node.Val = val
		}

		node.DownPtr = below

		predecessor := predecessors[level]
		// simple singly linked list rewiring
		node.RightPtr = predecessor.RightPtr
		predecessor.RightPtr = node

		below = node // this will be set for the next level
	}

	return nil
}

// NOTE: Should not be used to physically remove keys from memtable.
func (sl *SkipList) Delete(targetKey string) error {
	// TODO: update size
	predecessors, found := sl.search(targetKey)

	if found == nil {
		return fmt.Errorf("%w. targetKey = %s", ErrKeyNotFound, targetKey)
	}

	// rewire the predecessors
	for _, predecessor := range predecessors {
		next := predecessor.RightPtr
		if next != nil && next.Key == targetKey {
			predecessor.RightPtr = next.RightPtr
		}
	}

	// remove empty top levels (bottom doesnt get removed)
	for sl.currHeight > 1 && sl.head.RightPtr == nil {
		sl.head = sl.head.DownPtr
		sl.currHeight--
	}

	return nil
}
