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
	t.Run("Proper", func(t *testing.T) {
		f := newFragmentSupport(maxConsensusMessageSize)
		assert.NotNil(t, f, "Expected new fragment support not nil")
		fragmenter := f.(*fragmentSupportImpl)
		assert.NotNil(t, fragmenter.holders, "Expected new fragment support have non-nil holders map")
		assert.Equal(t, 0, len(fragmenter.holders), "Expected new fragment support have an empty holders map")
		assert.NotNil(t, fragmenter.holderMapByFragmentKey, "Expected new fragment support have non-nil holderMapByFragmentKey")
		assert.Equal(t, 0, len(fragmenter.holderMapByFragmentKey), "Expected new fragment support have empty holderMapByFragmentKey")
		assert.NotNil(t, fragmenter.holderListByAge, "Expected new fragment support have non-nil holderListByAge")
		assert.Equal(t, 0, fragmenter.holderListByAge.Len(), "Expected new fragment support have empty holderListByAge")
	})

	t.Run("NonPositiveFragmentSize", func(t *testing.T) {
		assert.Nil(t, newFragmentSupport(0), "Expected newFragmentSupport returns nil when fragmentSize is 0")
		assert.Nil(t, newFragmentSupport(-10), "Expected newFragmentSupport returns nil when fragmentSize is 0")
	})
}

func TestMakeFragments(t *testing.T) {
	fragmenter := newFragmentSupport(maxConsensusMessageSize)
	fragmentKey := []byte("test fragment key")

	t.Run("OneFullFragment", func(t *testing.T) {
		// should produce just 1 fragment
		data := make([]byte, maxConsensusMessageSize)
		fragments := fragmenter.MakeFragments(data, fragmentKey, 0)
		assert.Equal(t, 1, len(fragments))
		f := fragments[0]
		assert.Equal(t, maxConsensusMessageSize, len(f.Fragment))
		assert.Equal(t, fragmentKey, f.FragmentKey)
		assert.Equal(t, uint64(0), f.FragmentId)
		assert.Equal(t, uint32(0), f.Sequence)
		assert.Equal(t, uint32(1), f.TotalFragments)
	})

	// should produce 6 fragments
	t.Run("MultipleFragments", func(t *testing.T) {
		data := make([]byte, 5*maxConsensusMessageSize+1)
		fragments := fragmenter.MakeFragments(data, fragmentKey, 0)
		assert.Equal(t, 6, len(fragments), "Expected 6 fragments")
		for i, f := range fragments {
			if i != 5 {
				assert.Equal(t, maxConsensusMessageSize, len(f.Fragment), fmt.Sprintf("length of data in fragment should be %d", maxConsensusMessageSize))
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
		fragmenter := newFragmentSupport(maxConsensusMessageSize)

		data := make([]byte, 5*maxConsensusMessageSize+1)
		fragments := fragmenter.MakeFragments(data, fragmentKey, 0)
		for i, f := range fragments {
			reassembled := fragmenter.Reassemble(f)
			if i != 5 {
				assert.Nil(t, reassembled, "reassemble result should be nil, except for the last fragment")
			} else {
				assert.NotNil(t, reassembled, "data should have been reassembled with all fragments")
				assert.Equal(t, 5*maxConsensusMessageSize+1, len(reassembled))
			}
		}
		assert.False(t, fragmenter.IsPending(), "Expected there is no pending message")
	})

	t.Run("OutOfOrder", func(t *testing.T) {
		fragmenter := newFragmentSupport(maxConsensusMessageSize)

		data := make([]byte, 5*maxConsensusMessageSize+1)
		fragments := fragmenter.MakeFragments(data, fragmentKey, 0)
		indices := []int{5, 1, 0, 4, 3, 2}
		for i, idx := range indices {
			reassembled := fragmenter.Reassemble(fragments[idx])
			if i != 5 {
				assert.Nil(t, reassembled, "reassemble result should be nil, except for the last fragment")
			} else {
				assert.NotNil(t, reassembled, "data should have been reassembled with all fragments")
				assert.Equal(t, 5*maxConsensusMessageSize+1, len(reassembled))
			}
		}
		assert.False(t, fragmenter.IsPending(), "Expected there is no pending message")
	})

	t.Run("OutOfBoundFragment", func(t *testing.T) {
		fragmenter := newFragmentSupport(maxConsensusMessageSize)

		data := make([]byte, 5*maxConsensusMessageSize+1)
		fragments := fragmenter.MakeFragments(data, fragmentKey, 0)
		indices := []int{5, 1, 0, 4, 2}
		for _, idx := range indices {
			fragmenter.Reassemble(fragments[idx])
		}

		fragments[3].Sequence = uint32(len(fragments))
		assert.Nil(t, fragmenter.Reassemble(fragments[3]), "Expected reassemble returns nil")
		assert.True(t, fragmenter.IsPending(), "Expected there is pending message")
	})

	t.Run("DuplicateFragment", func(t *testing.T) {
		fragmenter := newFragmentSupport(maxConsensusMessageSize)

		data := make([]byte, 5*maxConsensusMessageSize+1)
		fragments := fragmenter.MakeFragments(data, fragmentKey, 0)
		assert.Nil(t, fragmenter.Reassemble(fragments[0]), "Expected reassemble returns nil")
		assert.Nil(t, fragmenter.Reassemble(fragments[0]), "Expected reassemble returns nil")
		assert.True(t, fragmenter.IsPending(), "Expected there is pending message")
	})
}

func TestIsPending(t *testing.T) {
	fragmentKey := []byte("test fragment key")
	t.Run("Empty", func(t *testing.T) {
		fragmenter := newFragmentSupport(maxConsensusMessageSize)
		assert.False(t, fragmenter.IsPending(), "Expected isPending returns false when there are no pending fragments")

		fragment := &ab.HcsMessageFragment{
			Fragment:       make([]byte, 2),
			FragmentKey:    fragmentKey,
			FragmentId:     0,
			Sequence:       0,
			TotalFragments: 1,
		}
		fragmenter.Reassemble(fragment)
		assert.False(t, fragmenter.IsPending(), "Expected isPending returns false after a single-fragment message is processed")
	})

	t.Run("NonEmpty", func(t *testing.T) {
		fragmenter := newFragmentSupport(maxConsensusMessageSize)
		fragment := &ab.HcsMessageFragment{
			Fragment:       make([]byte, 2),
			FragmentKey:    fragmentKey,
			FragmentId:     0,
			Sequence:       0,
			TotalFragments: 2,
		}
		fragmenter.Reassemble(fragment)
		assert.True(t, fragmenter.IsPending(), "Expected isPending returns true when there are pending fragments")
	})
}

func TestExpireByAge(t *testing.T) {
	fragmentKey := []byte("test fragment key")
	fragmentID := uint64(1)
	fragmenter := newFragmentSupport(maxConsensusMessageSize)

	// first segment of a multi-segment message
	fragmenter.Reassemble(&ab.HcsMessageFragment{
		Fragment:       make([]byte, 1),
		FragmentKey:    fragmentKey,
		FragmentId:     fragmentID,
		Sequence:       0,
		TotalFragments: 2,
	})
	fragmentID++

	// reassemble 6 single-fragment messages
	for i := 0; i < 6; i++ {
		fragmenter.Reassemble(&ab.HcsMessageFragment{
			Fragment:       make([]byte, 1),
			FragmentKey:    fragmentKey,
			FragmentId:     fragmentID,
			Sequence:       0,
			TotalFragments: 1,
		})
		fragmentID++
	}

	// first segment of another multi-segment message
	fragmenter.Reassemble(&ab.HcsMessageFragment{
		Fragment:       make([]byte, 1),
		FragmentKey:    fragmentKey,
		FragmentId:     fragmentID,
		Sequence:       0,
		TotalFragments: 2,
	})
	fragmentID++

	// reassemble 3 single-fragment messages
	for i := 0; i < 3; i++ {
		fragmenter.Reassemble(&ab.HcsMessageFragment{
			Fragment:       make([]byte, 1),
			FragmentKey:    fragmentKey,
			FragmentId:     fragmentID,
			Sequence:       0,
			TotalFragments: 1,
		})
		fragmentID++
	}

	// first fragment's holder is at age 10
	assert.Equal(t, 1, fragmenter.ExpireByAge(10))
	assert.True(t, fragmenter.IsPending())

	// the other fragment's holder is at age 3
	assert.Equal(t, 1, fragmenter.ExpireByAge(3))
	assert.False(t, fragmenter.IsPending())
}

func TestExpireByFragmentKey(t *testing.T) {
	t.Run("NilFragmentKey", func(t *testing.T) {
		fragmenter := newFragmentSupport(maxConsensusMessageSize)
		count, err := fragmenter.ExpireByFragmentKey(nil)
		assert.Equal(t, 0, count, "Expected expireByFragmentKey returns 0 count with nil fragmentKey")
		assert.Error(t, err, "Expected expireByFragmentKey returns error with nil fragmentKey")
	})

	t.Run("NonExistingFragmentKey", func(t *testing.T) {
		fragmenter := newFragmentSupport(maxConsensusMessageSize)
		count, err := fragmenter.ExpireByFragmentKey([]byte("dummy key"))
		assert.Equal(t, 0, count, "Expected 0 messages expired when provided with non-existing fragmentKey")
		assert.NoError(t, err, "Expected expireByFragmentKey returns no error with non-existing fragmentKey")
	})

	t.Run("Proper", func(t *testing.T) {
		fragmentKey1 := []byte("test fragment key 1")
		fragmentID1 := uint64(1)
		fragmentKey2 := []byte("test fragment key 2")
		fragmentID2 := uint64(1)

		fragmenter := newFragmentSupport(maxConsensusMessageSize)

		// 6 fragments with fragmentKey1
		for i := 0; i < 6; i++ {
			fragmenter.Reassemble(&ab.HcsMessageFragment{
				Fragment:       make([]byte, 1),
				FragmentKey:    fragmentKey1,
				FragmentId:     fragmentID1,
				Sequence:       0,
				TotalFragments: 2,
			})
			fragmentID1++
		}

		// 9 fragments with fragmentKey2
		for i := 0; i < 9; i++ {
			fragmenter.Reassemble(&ab.HcsMessageFragment{
				Fragment:       make([]byte, 1),
				FragmentKey:    fragmentKey2,
				FragmentId:     fragmentID2,
				Sequence:       0,
				TotalFragments: 2,
			})
			fragmentID2++
		}

		assert.True(t, fragmenter.IsPending())
		count, err := fragmenter.ExpireByFragmentKey(fragmentKey1)
		assert.Equal(t, 6, count)
		assert.NoError(t, err)

		assert.True(t, fragmenter.IsPending())
		count, err = fragmenter.ExpireByFragmentKey(fragmentKey2)
		assert.Equal(t, 9, count)
		assert.NoError(t, err)
		assert.False(t, fragmenter.IsPending())
	})
}

func TestCalcAge(t *testing.T) {
	assert.Equal(t, uint64(0), calcAge(100, 100), "Expected age to be 0 when bornTick and currentTick equal")
	assert.Equal(t, uint64(10), calcAge(100, 110), "Expected age to be 10")
	assert.Equal(t, uint64(6), calcAge(^uint64(1)-3, 2), "Expected age to be 6")
}
