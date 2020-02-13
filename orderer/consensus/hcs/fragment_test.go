/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestEmptyFragmentSupport(t *testing.T) {
	fragmenter := newFragmentSupport()

	assert.NotNil(t, fragmenter.holders, "new fragment support should have non-nil holders map")
	assert.Equal(t, 0, len(fragmenter.holders), "new fragment support should have an empty holders map")
}

func TestMakeFragments(t *testing.T) {
	fragmenter := newFragmentSupport()

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

	// should produce 6 fragments
	data = make([]byte, 5 * fragmentSize + 1)
	fragments = fragmenter.makeFragments(data, "testKey", 0)
	assert.Equal(t, 6, len(fragments), "expect 6 fragments")
	for i, f := range fragments {
		if i !=  5 {
			assert.Equal(t, fragmentSize, len(f.Fragment), fmt.Sprintf("length of data in fragment should be %d", fragmentSize))
		} else {
			assert.Equal(t, 1, len(f.Fragment), "length of data in last fragment should be 1")
		}
		assert.Equal(t, "testKey", f.FragmentKey)
		assert.Equal(t, uint64(0), f.FragmentId)
		assert.Equal(t, uint32(i), f.Sequence)
		assert.Equal(t, uint32(6), f.TotalFragments)
	}
}

func TestReassembly(t *testing.T) {
	fragmenter := newFragmentSupport()

	data := make([]byte, 5 * fragmentSize + 1)
	fragments := fragmenter.makeFragments(data, "testKey", 0)
	for i, f := range fragments {
		reassembled := fragmenter.reassemble(f)
		if i != 5 {
			assert.Nil(t, reassembled, "reassemble result should be nil, except for the last fragment")
		} else {
			assert.NotNil(t, reassembled, "data should have been reassembled with all fragments")
			assert.Equal(t, 5 * fragmentSize + 1, len(reassembled))
		}
	}
	assert.Equal(t, 0, len(fragmenter.holders), "holders should be empty after fragments are assembled")
}

func TestOutOfOrderReassembly(t *testing.T) {
	fragmenter := newFragmentSupport()

	data := make([]byte, 5 * fragmentSize + 1)
	fragments := fragmenter.makeFragments(data, "testKey", 0)
	indices := []int{5, 1, 0, 4, 3, 2}
	for i, idx := range indices {
		reassembled := fragmenter.reassemble(fragments[idx])
		if i != 5 {
			assert.Nil(t, reassembled, "reassemble result should be nil, except for the last fragment")
		} else {
			assert.NotNil(t, reassembled, "data should have been reassembled with all fragments")
			assert.Equal(t, 5 * fragmentSize + 1, len(reassembled))
		}
	}
	assert.Equal(t, 0, len(fragmenter.holders), "holders should be empty after fragments are assembled")
}