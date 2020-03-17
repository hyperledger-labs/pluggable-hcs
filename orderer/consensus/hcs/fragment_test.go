/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"encoding/hex"
	"fmt"
	"testing"

	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/stretchr/testify/assert"
)

func TestEmptyFragmentSupport(t *testing.T) {
	fragmenter := newFragmentSupport()

	assert.NotNil(t, fragmenter.holders, "Expected new fragment support have non-nil holders map")
	assert.Equal(t, 0, len(fragmenter.holders), "Expected new fragment support have an empty holders map")
	assert.NotNil(t, fragmenter.holderMapByFragmentKey, "Expected new fragment support have non-nil holderMapByFragmentKey")
	assert.Equal(t, 0, len(fragmenter.holderMapByFragmentKey), "Expected new fragment support have empty holderMapByFragmentKey")
	assert.NotNil(t, fragmenter.holderListByAge, "Expected new fragment support have non-nil holderListByAge")
	assert.Equal(t, 0, fragmenter.holderListByAge.Len(), "Expected new fragment support have empty holderListByAge")
}

func TestMakeFragments(t *testing.T) {
	fragmenter := newFragmentSupport()
	fragmentKey := []byte("test fragment key")

	t.Run("OneFullFragment", func(t *testing.T) {
		// should produce just 1 fragment
		data := make([]byte, fragmentSize)
		fragments := fragmenter.makeFragments(data, fragmentKey, 0)
		assert.Equal(t, 1, len(fragments))
		f := fragments[0]
		assert.Equal(t, fragmentSize, len(f.Fragment))
		assert.Equal(t, fragmentKey, f.FragmentKey)
		assert.Equal(t, uint64(0), f.FragmentId)
		assert.Equal(t, uint32(0), f.Sequence)
		assert.Equal(t, uint32(1), f.TotalFragments)
	})

	// should produce 6 fragments
	t.Run("MultipleFragments", func(t *testing.T) {
		data := make([]byte, 5*fragmentSize+1)
		fragments := fragmenter.makeFragments(data, fragmentKey, 0)
		assert.Equal(t, 6, len(fragments), "Expected 6 fragments")
		for i, f := range fragments {
			if i != 5 {
				assert.Equal(t, fragmentSize, len(f.Fragment), fmt.Sprintf("length of data in fragment should be %d", fragmentSize))
			} else {
				assert.Equal(t, 1, len(f.Fragment), "length of data in last fragment should be 1")
			}
			assert.Equal(t, fragmentKey, f.FragmentKey)
			assert.Equal(t, uint64(0), f.FragmentId)
			assert.Equal(t, uint32(i), f.Sequence)
			assert.Equal(t, uint32(6), f.TotalFragments)
		}
	})
}

func TestReassembly(t *testing.T) {
	fragmentKey := []byte("test fragment key")
	t.Run("Proper", func(t *testing.T) {
		fragmenter := newFragmentSupport()

		data := make([]byte, 5*fragmentSize+1)
		fragments := fragmenter.makeFragments(data, fragmentKey, 0)
		for i, f := range fragments {
			reassembled := fragmenter.reassemble(f)
			if i != 5 {
				assert.Nil(t, reassembled, "reassemble result should be nil, except for the last fragment")
			} else {
				assert.NotNil(t, reassembled, "data should have been reassembled with all fragments")
				assert.Equal(t, 5*fragmentSize+1, len(reassembled))
			}
		}
		assert.Equal(t, 0, len(fragmenter.holders), "holders should be empty after fragments are assembled")
	})

	t.Run("OutOfOrder", func(t *testing.T) {
		fragmenter := newFragmentSupport()

		data := make([]byte, 5*fragmentSize+1)
		fragments := fragmenter.makeFragments(data, fragmentKey, 0)
		indices := []int{5, 1, 0, 4, 3, 2}
		for i, idx := range indices {
			reassembled := fragmenter.reassemble(fragments[idx])
			if i != 5 {
				assert.Nil(t, reassembled, "reassemble result should be nil, except for the last fragment")
			} else {
				assert.NotNil(t, reassembled, "data should have been reassembled with all fragments")
				assert.Equal(t, 5*fragmentSize+1, len(reassembled))
			}
		}
		assert.Equal(t, 0, len(fragmenter.holders), "holders should be empty after fragments are assembled")
	})

	t.Run("OutOfBoundFragment", func(t *testing.T) {
		fragmenter := newFragmentSupport()

		data := make([]byte, 5*fragmentSize+1)
		fragments := fragmenter.makeFragments(data, fragmentKey, 0)
		indices := []int{5, 1, 0, 4, 2}
		for _, idx := range indices {
			fragmenter.reassemble(fragments[idx])
		}

		fragments[3].Sequence = uint32(len(fragments))
		assert.Nil(t, fragmenter.reassemble(fragments[3]), "Expected reassemble returns nil")
		assert.Equal(t, 1, len(fragmenter.holders), "Expected holders should have length 1")
		for _, holder := range fragmenter.holders {
			assert.Equal(t, uint32(5), holder.count, "Expected five fragments processed")
			assert.Nil(t, holder.fragments[3], "Expected fragment 3 in holder to be nil")
		}
	})

	t.Run("DuplicateFragment", func(t *testing.T) {
		fragmenter := newFragmentSupport()

		data := make([]byte, 5*fragmentSize+1)
		fragments := fragmenter.makeFragments(data, fragmentKey, 0)
		assert.Nil(t, fragmenter.reassemble(fragments[0]), "Expected reassemble returns nil")
		assert.Nil(t, fragmenter.reassemble(fragments[0]), "Expected reassemble returns nil")
		assert.Equal(t, 1, len(fragmenter.holders), "Expected holders have length 1")
		for _, holder := range fragmenter.holders {
			assert.Equal(t, uint32(1), holder.count, "Expected one fragment processed")
		}
	})
}

func TestIsPending(t *testing.T) {
	fragmentKey := []byte("test fragment key")
	t.Run("Empty", func(t *testing.T) {
		fragmenter := newFragmentSupport()
		assert.False(t, fragmenter.isPending(), "Expected isPending returns false when there are no pending fragments")

		fragment := &ab.HcsMessageFragment{
			Fragment:       make([]byte, 2),
			FragmentKey:    fragmentKey,
			FragmentId:     0,
			Sequence:       0,
			TotalFragments: 1,
		}
		fragmenter.reassemble(fragment)
		assert.False(t, fragmenter.isPending(), "Expected isPending returns false after a single-fragment message is processed")
	})

	t.Run("NonEmpty", func(t *testing.T) {
		fragmenter := newFragmentSupport()
		fragment := &ab.HcsMessageFragment{
			Fragment:       make([]byte, 2),
			FragmentKey:    fragmentKey,
			FragmentId:     0,
			Sequence:       0,
			TotalFragments: 2,
		}
		fragmenter.reassemble(fragment)
		assert.True(t, fragmenter.isPending(), "Expected isPending returns true when there are pending fragments")
	})
}

func TestExpireByAge(t *testing.T) {
	fragmentKey := []byte("test fragment key")
	fragmentKeyStr := hex.EncodeToString(fragmentKey)

	t.Run("PendingFragmentsFromTwoMessages", func(t *testing.T) {
		fragmenter := newFragmentSupport()

		fragment1 := &ab.HcsMessageFragment{
			Fragment:       make([]byte, 2),
			FragmentKey:    fragmentKey,
			FragmentId:     0,
			Sequence:       0,
			TotalFragments: 2,
		}
		fragmenter.reassemble(fragment1)
		assert.Len(t, fragmenter.holderMapByFragmentKey, 1, "Expected holderMapByFragmentKey has one entry")
		holderMap := fragmenter.holderMapByFragmentKey[fragmentKeyStr]
		assert.NotNil(t, holderMap, "Expected fragmentKey in holderMapByFragmentKey")
		assert.Len(t, holderMap, 1, "Expected holderMap has one entry")
		holderKey1 := makeHolderKey(fragment1.FragmentKey, fragment1.FragmentId)
		assert.NotNil(t, holderMap[holderKey1], "Expected holderKey1 in holderMap")
		assert.Equal(t, 1, fragmenter.holderListByAge.Len(), "Expected holderListByAge has one element")
		holder := fragmenter.holderListByAge.Front().Value.(*fragmentHolder)
		assert.Equal(t, holderKey1, holder.key, "Expected holder key equals")

		fragment2 := &ab.HcsMessageFragment{
			Fragment:       make([]byte, 2),
			FragmentKey:    fragmentKey,
			FragmentId:     1,
			Sequence:       0,
			TotalFragments: 2,
		}
		fragmenter.reassemble(fragment2)
		assert.Len(t, fragmenter.holderMapByFragmentKey, 1, "Expected holderMapByFragmentKey has one entry")
		holderMap = fragmenter.holderMapByFragmentKey[fragmentKeyStr]
		assert.NotNil(t, holderMap, "Expected fragmentKey in holderMapByFragmentKey")
		assert.Len(t, holderMap, 2, "Expected holderMap has two entries")
		holderKey2 := makeHolderKey(fragment2.FragmentKey, fragment2.FragmentId)
		assert.NotNil(t, holderMap[holderKey2], "Expected holderKey2 in holderMap")
		assert.Equal(t, 2, fragmenter.holderListByAge.Len(), "Expected holderListByAge has two elements")
		holder = fragmenter.holderListByAge.Back().Value.(*fragmentHolder)
		assert.Equal(t, holderKey2, holder.key, "Expected holder key equals")
	})

	t.Run("ProperExpire", func(t *testing.T) {
		fragmenter := newFragmentSupport()

		fragmentID := uint64(1)
		// first segment of a multi-segment message
		fragmenter.reassemble(&ab.HcsMessageFragment{
			Fragment:       make([]byte, 1),
			FragmentKey:    fragmentKey,
			FragmentId:     fragmentID,
			Sequence:       0,
			TotalFragments: 2,
		})
		fragmentID++

		// reassemble 6 single-fragment messages
		for i := 0; i < 6; i++ {
			fragmenter.reassemble(&ab.HcsMessageFragment{
				Fragment:       make([]byte, 1),
				FragmentKey:    fragmentKey,
				FragmentId:     fragmentID,
				Sequence:       0,
				TotalFragments: 1,
			})
			fragmentID++
		}
		assert.Len(t, fragmenter.holders, 1, "Expected 1 message pending fully reassemble")
		assert.Len(t, fragmenter.holderMapByFragmentKey, 1, "Expected holderMapByFragmentKey has one entry")
		holderMap := fragmenter.holderMapByFragmentKey[fragmentKeyStr]
		assert.NotNil(t, holderMap, "Expected fragmentKey in holderMapByFragmentKey")
		assert.Len(t, holderMap, 1, "Expected holderMap has one entry")
		assert.Equal(t, 1, fragmenter.holderListByAge.Len(), "Expected holderListByAge has one entry")

		// first segment of another multi-segment message
		fragmenter.reassemble(&ab.HcsMessageFragment{
			Fragment:       make([]byte, 1),
			FragmentKey:    fragmentKey,
			FragmentId:     fragmentID,
			Sequence:       0,
			TotalFragments: 2,
		})
		fragmentID++

		for i := 0; i < 3; i++ {
			fragmenter.reassemble(&ab.HcsMessageFragment{
				Fragment:       make([]byte, 1),
				FragmentKey:    fragmentKey,
				FragmentId:     fragmentID,
				Sequence:       0,
				TotalFragments: 1,
			})
			fragmentID++
		}
		assert.Equal(t, 2, fragmenter.holderListByAge.Len(), "Expected holderListByAge has 2 elements")
		assert.Len(t, fragmenter.holders, 2, "Expected 2 messages pending fully reassemble")

		// first fragment's holder is at age 10
		fragmenter.expireByAge(10)
		assert.Equal(t, 1, fragmenter.holderListByAge.Len(), "Expected holderListByAge has 1 element")
		assert.Len(t, fragmenter.holders, 1, "Expected 1 message pending fully reassemble")

		fragmenter.expireByAge(3)
		assert.Equal(t, 0, fragmenter.holderListByAge.Len(), "Expected holderListByAge is empty")
		assert.Len(t, fragmenter.holders, 0, "Expected empty holders")
	})

	t.Run("ExpireWithZeroMaxAge", func(t *testing.T) {
		fragmenter := newFragmentSupport()
		assert.NotPanics(t, func() { fragmenter.expireByAge(0) }, "Expect expire called with maxAge=0 return without panics")
	})
}

func TestExpireByFragmentKey(t *testing.T) {
	t.Run("NilFragmentKey", func(t *testing.T) {
		fragmenter := newFragmentSupport()
		count, err := fragmenter.expireByFragmentKey(nil)
		assert.Equal(t, 0, count, "Expected expireByFragmentKey returns 0 count with nil fragmentKey")
		assert.Error(t, err, "Expected expireByFragmentKey returns error with nil fragmentKey")
	})

	t.Run("NonExistingFragmentKey", func(t *testing.T) {
		fragmenter := newFragmentSupport()
		count, err := fragmenter.expireByFragmentKey([]byte("dummy key"))
		assert.Equal(t, 0, count, "Expected 0 messages expired when provided with non-existing fragmentKey")
		assert.NoError(t, err, "Expected expireByFragmentKey returns no error with non-existing fragmentKey")
	})

	t.Run("Proper", func(t *testing.T) {
		fragmentKey1 := []byte("test fragment key 1")
		fragmentID1 := uint64(1)
		fragmentKey2 := []byte("test fragment key 2")
		fragmentID2 := uint64(1)

		fragmenter := newFragmentSupport()

		for i := 0; i < 6; i++ {
			fragmenter.reassemble(&ab.HcsMessageFragment{
				Fragment:       make([]byte, 1),
				FragmentKey:    fragmentKey1,
				FragmentId:     fragmentID1,
				Sequence:       0,
				TotalFragments: 2,
			})
			fragmentID1++
		}

		for i := 0; i < 9; i++ {
			fragmenter.reassemble(&ab.HcsMessageFragment{
				Fragment:       make([]byte, 1),
				FragmentKey:    fragmentKey2,
				FragmentId:     fragmentID2,
				Sequence:       0,
				TotalFragments: 2,
			})
			fragmentID2++
		}

		assert.Equal(t, 15, fragmenter.holderListByAge.Len(), "Expected holderListByAge has 15 elements")
		assert.Len(t, fragmenter.holderMapByFragmentKey, 2, "Expected holderMapByFragmentKey has 2 entries")

		fragmenter.expireByFragmentKey(fragmentKey1)
		assert.Equal(t, 9, fragmenter.holderListByAge.Len(), "Expected holderListByAge has 9 elements")
		assert.Len(t, fragmenter.holderMapByFragmentKey, 1, "Expected holderMapByFragmentKey has 1 entry")

		fragmenter.expireByFragmentKey(fragmentKey2)
		assert.Equal(t, 0, fragmenter.holderListByAge.Len(), "Expected holderListByAge is empty")
		assert.Len(t, fragmenter.holderMapByFragmentKey, 0, "Expected holderMapByFragmentKey is empty")
	})
}

func TestCalcAge(t *testing.T) {
	assert.Equal(t, uint64(0), calcAge(100, 100), "Expected age to be 0 when bornTick and currentTick equal")
	assert.Equal(t, uint64(10), calcAge(100, 110), "Expected age to be 10")
	assert.Equal(t, uint64(6), calcAge(^uint64(1)-3, 2), "Expected age to be 6")
}
