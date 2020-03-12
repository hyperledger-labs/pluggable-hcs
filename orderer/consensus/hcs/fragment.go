/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"fmt"

	ab "github.com/hyperledger/fabric-protos-go/orderer"
)

const fragmentSize = 3800

func newFragmentSupport() *fragmentSupport {
	return &fragmentSupport{
		holders:    map[string]*fragmentHolder{},
		holderAges: []map[string]*holderAgeInfo{},
	}
}

type fragmentSupport struct {
	holders    map[string]*fragmentHolder
	holderAges []map[string]*holderAgeInfo
}

func (processor *fragmentSupport) isPending() bool {
	return len(processor.holders) != 0
}

func (processor *fragmentSupport) ageAll(holder *fragmentHolder, holderKey string, completed bool) {
	if holder != nil {
		if holder.ageInfo.ageInfoRef != nil {
			// delete it from the current holderAgeInfo map
			delete(holder.ageInfo.ageInfoRef, holderKey)
		}
		// create a new age zero entry and prepend age zero to the array
		var ageZero map[string]*holderAgeInfo
		if !completed {
			// the holder is pending complete reassembly
			ageZero = map[string]*holderAgeInfo{holderKey: &holder.ageInfo}
		}
		holder.ageInfo.ageInfoRef = ageZero
		processor.holderAges = append([]map[string]*holderAgeInfo{ageZero}, processor.holderAges...)
	} else {
		// aging caused by a single-fragment message, so holder is nil
		processor.holderAges = append([]map[string]*holderAgeInfo{nil}, processor.holderAges...)
	}
}

func (processor *fragmentSupport) expire(maxAge int) {
	if maxAge <= 0 {
		logger.Errorf("invalid maxAge - %d", maxAge)
		return
	}

	lastIndex := len(processor.holderAges) - 1
	for {
		if lastIndex < maxAge {
			break
		}

		if oldest := processor.holderAges[lastIndex]; oldest != nil {
			for holderKey := range oldest {
				if _, ok := processor.holders[holderKey]; !ok {
					logger.Errorf("unable to find fragment holder with key \"%s\"", holderKey)
				} else {
					delete(processor.holders, holderKey)
					logger.Infof("drop fragments with key(%s) per the expiring policy", holderKey)
				}
			}
		}
		processor.holderAges[lastIndex] = nil
		processor.holderAges = processor.holderAges[:lastIndex]
		lastIndex--
	}

	// remove consecutive trailing empty/nil entries
	for {
		if lastIndex < 0 ||
			(processor.holderAges[lastIndex] != nil && len(processor.holderAges[lastIndex]) != 0) {
			break
		}

		processor.holderAges[lastIndex] = nil
		processor.holderAges = processor.holderAges[:lastIndex]
		lastIndex--
	}
}

func (processor *fragmentSupport) reassemble(fragment *ab.HcsMessageFragment) (reassembled []byte) {
	holderKey := ""
	isFragmentValid := true
	var holder *fragmentHolder
	completed := false
	defer func() {
		if isFragmentValid {
			processor.ageAll(holder, holderKey, completed)
		}
	}()

	if fragment.TotalFragments == 1 {
		reassembled = fragment.Fragment
		completed = true
		return
	}

	holderKey = makeHolderKey(fragment.FragmentKey, fragment.FragmentId)
	holder, ok := processor.holders[holderKey]
	if !ok {
		holder = newFragmentHolder(fragment.TotalFragments)
		processor.holders[holderKey] = holder
	}

	sequence := fragment.Sequence
	if sequence >= uint32(len(holder.fragments)) {
		logger.Errorf("fragment sequence %d is out of bound %d", sequence, len(holder.fragments))
		isFragmentValid = false
		return
	}

	if holder.fragments[sequence] != nil {
		logger.Warningf("bug, duplicate fragment at index %d received", sequence)
		isFragmentValid = false
		return
	}

	holder.fragments[sequence] = fragment
	holder.count++
	holder.size += uint32(len(fragment.Fragment))

	logger.Debugf("processed %d fragment of fragment id %d, total of %d fragments, %d bytes of data",
		sequence, fragment.FragmentId, fragment.TotalFragments, holder.size)
	if holder.count == uint32(len(holder.fragments)) {
		reassembled = make([]byte, 0, holder.size)
		for _, f := range holder.fragments {
			reassembled = append(reassembled, f.Fragment...)
			logger.Debugf("seq %d, add %d bytes, payload size is now %d, cap %d", f.Sequence, len(f.Fragment), len(reassembled), cap(reassembled))
		}
		delete(processor.holders, holderKey)
		completed = true
	}
	return
}

func (processor *fragmentSupport) makeFragments(data []byte, fragmentKey string, fragmentID uint64) []*ab.HcsMessageFragment {
	fragments := make([]*ab.HcsMessageFragment, (len(data)+fragmentSize-1)/fragmentSize)
	for i := 0; i < len(fragments); i++ {
		first := i * fragmentSize
		last := (i + 1) * fragmentSize
		if last > len(data) {
			last = len(data)
		}
		fragments[i] = &ab.HcsMessageFragment{
			Fragment:       data[first:last],
			FragmentKey:    fragmentKey,
			FragmentId:     fragmentID,
			Sequence:       uint32(i),
			TotalFragments: uint32(len(fragments)),
		}
	}
	return fragments
}

type fragmentHolder struct {
	fragments []*ab.HcsMessageFragment
	count     uint32
	size      uint32
	ageInfo   holderAgeInfo
}

func newFragmentHolder(total uint32) *fragmentHolder {
	return &fragmentHolder{fragments: make([]*ab.HcsMessageFragment, total)}
}

type holderAgeInfo struct {
	ageInfoRef map[string]*holderAgeInfo
}

// helper functions
func makeHolderKey(fragmentKey string, fragmentID uint64) string {
	return fmt.Sprintf("%s%d", fragmentKey, fragmentID)
}
