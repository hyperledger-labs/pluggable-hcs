/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"fmt"
	"testing"

	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/stretchr/testify/assert"
)

func TestEmptyFragmentSupport(t *testing.T) {
	fragmenter := newFragmentSupport()

	assert.NotNil(t, fragmenter.holders, "new fragment support should have non-nil holders map")
	assert.Equal(t, 0, len(fragmenter.holders), "new fragment support should have an empty holders map")
}

func TestMakeFragments(t *testing.T) {
	fragmenter := newFragmentSupport()

	t.Run("OneFullFragment", func(t *testing.T) {
		// should produce just 1 fragment
		data := make([]byte, fragmentSize)
		fragments := fragmenter.makeFragments(data, "testKey", 0)
		assert.Equal(t, 1, len(fragments))
		f := fragments[0]
		assert.Equal(t, fragmentSize, len(f.Fragment))
		assert.Equal(t, "testKey", f.FragmentKey)
		assert.Equal(t, uint64(0), f.FragmentId)
		assert.Equal(t, uint32(0), f.Sequence)
		assert.Equal(t, uint32(1), f.TotalFragments)
	})

	// should produce 6 fragments
	t.Run("MultipleFragments", func(t *testing.T) {
		data := make([]byte, 5*fragmentSize+1)
		fragments := fragmenter.makeFragments(data, "testKey", 0)
		assert.Equal(t, 6, len(fragments), "Expected 6 fragments")
		for i, f := range fragments {
			if i != 5 {
				assert.Equal(t, fragmentSize, len(f.Fragment), fmt.Sprintf("length of data in fragment should be %d", fragmentSize))
			} else {
				assert.Equal(t, 1, len(f.Fragment), "length of data in last fragment should be 1")
			}
			assert.Equal(t, "testKey", f.FragmentKey)
			assert.Equal(t, uint64(0), f.FragmentId)
			assert.Equal(t, uint32(i), f.Sequence)
			assert.Equal(t, uint32(6), f.TotalFragments)
		}
	})
}

func TestReassembly(t *testing.T) {
	t.Run("Proper", func(t *testing.T) {
		fragmenter := newFragmentSupport()

		data := make([]byte, 5*fragmentSize+1)
		fragments := fragmenter.makeFragments(data, "testKey", 0)
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
		fragments := fragmenter.makeFragments(data, "testKey", 0)
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
		fragments := fragmenter.makeFragments(data, "testKey", 0)
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
		fragments := fragmenter.makeFragments(data, "testKey", 0)
		assert.Nil(t, fragmenter.reassemble(fragments[0]), "Expected reassemble returns nil")
		assert.Nil(t, fragmenter.reassemble(fragments[0]), "Expected reassemble returns nil")
		assert.Equal(t, 1, len(fragmenter.holders), "Expected holders have length 1")
		for _, holder := range fragmenter.holders {
			assert.Equal(t, uint32(1), holder.count, "Expected one fragment processed")
		}
	})
}

func TestIsPending(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		fragmenter := newFragmentSupport()
		assert.False(t, fragmenter.isPending(), "Expected isPending returns false when there are no pending fragments")

		fragment := &ab.HcsMessageFragment{
			Fragment:       make([]byte, 2),
			FragmentKey:    "test fragment key",
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
			FragmentKey:    "test fragment key",
			FragmentId:     0,
			Sequence:       0,
			TotalFragments: 2,
		}
		fragmenter.reassemble(fragment)
		assert.True(t, fragmenter.isPending(), "Expected isPending returns true when there are pending fragments")
	})
}

func TestExpire(t *testing.T) {
	t.Run("NoPendingFragments", func(t *testing.T) {
		fragmenter := newFragmentSupport()
		assert.Len(t, fragmenter.holderAges, 0, "Expected empty holderAges with new fragment support")
		fragmenter.expire(1)
		assert.Len(t, fragmenter.holderAges, 0, "Expected empty holderAges after expire is called")
	})

	t.Run("PendingFragmentsFrom2Messages", func(t *testing.T) {
		fragmenter := newFragmentSupport()

		fragment1 := &ab.HcsMessageFragment{
			Fragment:       make([]byte, 2),
			FragmentKey:    "testKey",
			FragmentId:     0,
			Sequence:       0,
			TotalFragments: 2,
		}
		fragmenter.reassemble(fragment1)
		assert.Len(t, fragmenter.holderAges, 1, "Expected holderAges has one entry")
		assert.NotNil(t, fragmenter.holderAges[0], "Expected entry 0 of holderAges to be non-nil")
		assert.Len(t, fragmenter.holderAges[0], 1, "Expected entry 0 map has one entry")
		holderKey1 := makeHolderKey(fragment1.FragmentKey, fragment1.FragmentId)
		assert.Contains(t, fragmenter.holderAges[0], holderKey1, "Expected fragment1's holderKey in the map")
		assert.Equal(t, fragmenter.holderAges[0][holderKey1].ageInfoRef, fragmenter.holderAges[0], "Expected ageInfoRef equals holderAges[0]")
		holderAge1 := fragmenter.holderAges[0]

		fragment2 := &ab.HcsMessageFragment{
			Fragment:       make([]byte, 2),
			FragmentKey:    "testKey",
			FragmentId:     1,
			Sequence:       0,
			TotalFragments: 2,
		}
		fragmenter.reassemble(fragment2)
		assert.Len(t, fragmenter.holderAges, 2, "Expected holderAges has two entries")
		assert.NotNil(t, fragmenter.holderAges[0], "Expected entry 0 of holderAges to be non-nil")
		assert.Len(t, fragmenter.holderAges[0], 1, "Expected entry 0 map has one entry")
		holderKey2 := makeHolderKey(fragment2.FragmentKey, fragment2.FragmentId)
		assert.Contains(t, fragmenter.holderAges[0], holderKey2, "Expected fragment2's holderKey in the map")
		assert.Equal(t, fragmenter.holderAges[0][holderKey2].ageInfoRef, fragmenter.holderAges[0], "Expected ageInfoRef equals holderAges[0]")
		assert.Equal(t, holderAge1, fragmenter.holderAges[1], "Expected fragment1's age entry moved to index 1")
		assert.Len(t, holderAge1, 1, "Expected holderAge1's size is still 1")
		assert.Contains(t, holderAge1, holderKey1, "Expected holderAge0 has fragment1's holderKey")
		assert.Equal(t, holderAge1[holderKey1].ageInfoRef, holderAge1, "Expected ageInfoRef equals holderAge1")
	})

	t.Run("ProperExpire", func(t *testing.T) {
		fragmenter := newFragmentSupport()

		fragmentID := uint64(1)
		// first segment of a multi-segment message
		fragmenter.reassemble(&ab.HcsMessageFragment{
			Fragment:       make([]byte, 1),
			FragmentKey:    "testKey",
			FragmentId:     fragmentID,
			Sequence:       0,
			TotalFragments: 2,
		})
		fragmentID++

		// reassemble 6 single-fragment messages
		for i := 0; i < 6; i++ {
			fragmenter.reassemble(&ab.HcsMessageFragment{
				Fragment:       make([]byte, 1),
				FragmentKey:    "testKey",
				FragmentId:     fragmentID,
				Sequence:       0,
				TotalFragments: 1,
			})
			fragmentID++
		}
		assert.Len(t, fragmenter.holderAges, 7, "Expected holderAges has 7 entries")
		assert.Len(t, fragmenter.holders, 1, "Expected 1 message pending fully reassemble")

		// first segment of another multi-segment message
		fragmenter.reassemble(&ab.HcsMessageFragment{
			Fragment:       make([]byte, 1),
			FragmentKey:    "testKey",
			FragmentId:     fragmentID,
			Sequence:       0,
			TotalFragments: 2,
		})
		fragmentID++

		for i := 0; i < 3; i++ {
			fragmenter.reassemble(&ab.HcsMessageFragment{
				Fragment:       make([]byte, 1),
				FragmentKey:    "testKey",
				FragmentId:     fragmentID,
				Sequence:       0,
				TotalFragments: 1,
			})
			fragmentID++
		}
		assert.Len(t, fragmenter.holderAges, 11, "Expected holderAges has 11 entries")
		assert.Len(t, fragmenter.holders, 2, "Expected 2 messages pending fully reassemble")

		// first entry is age 0
		fragmenter.expire(10)
		assert.True(t, len(fragmenter.holderAges) >= 4, "Expected holderAges has at least 4 entries")
		assert.Len(t, fragmenter.holders, 1, "Expected 1 message pending fully reassemble")

		fragmenter.expire(3)
		assert.True(t, len(fragmenter.holderAges) < 4, "Expected holderAges has less than 4 entries")
		assert.Len(t, fragmenter.holders, 0, "Expected empty holders")
	})

	t.Run("ExpireWithNonPositiveMaxAge", func(t *testing.T) {
		fragmenter := newFragmentSupport()
		assert.NotPanics(t, func() { fragmenter.expire(0) }, "Expect expire called with maxAge=0 return without panics")
		assert.NotPanics(t, func() { fragmenter.expire(-10) }, "Expect expire called with negative maxAge return without panics")
	})
}
