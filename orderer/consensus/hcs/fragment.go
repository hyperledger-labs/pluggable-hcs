/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"container/list"
	"encoding/hex"
	"fmt"

	ab "github.com/hyperledger/fabric-protos-go/orderer"
)

const fragmentSize = 3800

func newFragmentSupport() *fragmentSupport {
	return &fragmentSupport{
		holders:                map[string]*fragmentHolder{},
		holderMapByFragmentKey: map[string]map[string]struct{}{},
		holderListByAge:        list.New(),
	}
}

type fragmentSupport struct {
	holders                map[string]*fragmentHolder
	holderMapByFragmentKey map[string]map[string]struct{}
	holderListByAge        *list.List
	tick                   uint64
}

func (processor *fragmentSupport) isPending() bool {
	return len(processor.holders) != 0
}

func (processor *fragmentSupport) expireByAge(maxAge uint64) {
	if maxAge == 0 {
		logger.Errorf("invalid maxAge - %d", maxAge)
		return
	}

	for {
		oldest := processor.holderListByAge.Front()
		if oldest == nil {
			break
		}
		holder := oldest.Value.(*fragmentHolder)
		age := calcAge(holder.tick, processor.tick)
		if age >= maxAge {
			processor.removeHolder(holder, true)
		} else {
			break
		}
	}
}

func (processor *fragmentSupport) expireByFragmentKey(fragmentKey []byte) (int, error) {
	if fragmentKey == nil {
		return 0, fmt.Errorf("nil fragmentKey is not allowed")
	}
	key := hex.EncodeToString(fragmentKey)
	count := 0
	if holderMap, ok := processor.holderMapByFragmentKey[key]; ok {
		delete(processor.holderMapByFragmentKey, key)
		count = len(holderMap)
		for holderKey := range holderMap {
			if holder, ok := processor.holders[holderKey]; ok {
				processor.removeHolder(holder, false)
			}
		}
	}
	return count, nil
}

func (processor *fragmentSupport) reassemble(fragment *ab.HcsMessageFragment) (reassembled []byte) {
	isValidFragment := false
	var holder *fragmentHolder
	defer func() {
		if isValidFragment {
			processor.tick++
			if holder != nil {
				holder.tick = processor.tick
			}
		}
	}()

	if fragment.TotalFragments == 1 {
		isValidFragment = true
		return fragment.Fragment
	}

	holderKey := makeHolderKey(fragment.FragmentKey, fragment.FragmentId)
	holder, ok := processor.holders[holderKey]
	if !ok {
		holder = newFragmentHolder(fragment.TotalFragments, holderKey)
		processor.holders[holderKey] = holder
		fragmentKey := hex.EncodeToString(fragment.FragmentKey)
		holderMap, ok := processor.holderMapByFragmentKey[fragmentKey]
		if !ok {
			holderMap = map[string]struct{}{}
			processor.holderMapByFragmentKey[fragmentKey] = holderMap
		}
		holderMap[holderKey] = struct{}{}
	}

	sequence := fragment.Sequence
	if sequence >= uint32(len(holder.fragments)) {
		logger.Errorf("fragment sequence %d is out of bound %d", sequence, len(holder.fragments))
		return
	}

	if holder.fragments[sequence] != nil {
		logger.Warningf("bug, duplicate fragment at index %d received", sequence)
		return
	}

	isValidFragment = true
	holder.fragments[sequence] = fragment
	holder.count++
	holder.size += uint32(len(fragment.Fragment))

	logger.Debugf("processed %d fragment of fragment id %d, total of %d fragments, %d bytes of data",
		sequence, fragment.FragmentId, fragment.TotalFragments, holder.size)
	if holder.count != uint32(len(holder.fragments)) {
		// the first element in holderListByAge is the oldest
		if holder.el != nil {
			processor.holderListByAge.MoveToBack(holder.el)
		} else {
			holder.el = processor.holderListByAge.PushBack(holder)
		}
	} else {
		reassembled = make([]byte, 0, holder.size)
		for _, f := range holder.fragments {
			reassembled = append(reassembled, f.Fragment...)
			logger.Debugf("seq %d, add %d bytes, payload size is now %d, cap %d", f.Sequence, len(f.Fragment), len(reassembled), cap(reassembled))
		}
		processor.removeHolder(holder, true)
		return
	}
	return
}

func (processor *fragmentSupport) removeHolder(holder *fragmentHolder, removeFromHolderMap bool) {
	delete(processor.holders, holder.key)
	if holder.el != nil {
		processor.holderListByAge.Remove(holder.el)
	}
	if removeFromHolderMap {
		if holderMap := processor.holderMapByFragmentKey[hex.EncodeToString(holder.fragments[0].FragmentKey)]; holderMap != nil {
			delete(holderMap, holder.key)
		}
	}
}

func (processor *fragmentSupport) makeFragments(data []byte, fragmentKey []byte, fragmentID uint64) []*ab.HcsMessageFragment {
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
	key       string
	count     uint32
	size      uint32
	tick      uint64
	el        *list.Element
}

func newFragmentHolder(total uint32, key string) *fragmentHolder {
	return &fragmentHolder{fragments: make([]*ab.HcsMessageFragment, total), key: key}
}

// helper functions
func makeHolderKey(fragmentKey []byte, fragmentID uint64) string {
	return fmt.Sprintf("%s%d", hex.EncodeToString(fragmentKey), fragmentID)
}

func calcAge(bornTick uint64, currentTick uint64) uint64 {
	if currentTick < bornTick {
		return ^uint64(1) - bornTick + currentTick + 1
	}
	return currentTick - bornTick
}
