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
	return &fragmentSupport{holders: make(map[string]*fragmentHolder)}
}

type fragmentSupport struct {
	holders map[string]*fragmentHolder
}

func (processor *fragmentSupport) reassemble(fragment *ab.HcsMessageFragment) []byte {
	if fragment.TotalFragments == 1 {
		return fragment.Fragment
	}

	holderKey := fmt.Sprintf("%s%d", fragment.FragmentKey, fragment.FragmentId)
	holder, ok := processor.holders[holderKey]
	if !ok {
		holder = newFragmentHolder(fragment.TotalFragments)
		processor.holders[holderKey] = holder
	}

	sequence := fragment.Sequence
	if sequence >= uint32(len(holder.fragments)) {
		logger.Panicf("fragment sequence %d is out of bound %d", sequence, len(holder.fragments))
	}

	if holder.fragments[sequence] != nil {
		logger.Warningf("bug, duplicate wrap at index %d received", sequence)
		return nil
	}

	holder.fragments[sequence] = fragment
	holder.count++
	holder.size += uint32(len(fragment.Fragment))

	logger.Debugf("processed %d fragment of fragment id %d, total of %d fragments, %d bytes of data",
		sequence, fragment.FragmentId, fragment.TotalFragments, holder.size)
	if holder.count == uint32(len(holder.fragments)) {
		payload := make([]byte, 0, holder.size)
		for _, f := range holder.fragments {
			payload = append(payload, f.Fragment...)
			logger.Debugf("seq %d, add %d bytes, payload size is now %d, cap %d", f.Sequence, len(f.Fragment), len(payload), cap(payload))
		}
		delete(processor.holders, holderKey)
		return payload
	} else {
		return nil
	}
}

func (processor *fragmentSupport) makeFragments(data []byte, fragmentKey string, fragmentId uint64) []*ab.HcsMessageFragment {
	fragments := make([]*ab.HcsMessageFragment, (len(data) + fragmentSize - 1) / fragmentSize)
	for i := 0; i < len(fragments); i++ {
		first := i * fragmentSize
		last := (i+1) * fragmentSize
		if last > len(data) {
			last = len(data)
		}
		fragments[i] = &ab.HcsMessageFragment{
			Fragment:             data[first:last],
			FragmentKey:          fragmentKey,
			FragmentId:           fragmentId,
			Sequence:             uint32(i),
			TotalFragments:       uint32(len(fragments)),
		}
	}
	return fragments
}


type fragmentHolder struct {
	fragments   []*ab.HcsMessageFragment
	count       uint32
	size        uint32
}

func newFragmentHolder(total uint32) *fragmentHolder{
	return &fragmentHolder{
		fragments: make([]*ab.HcsMessageFragment, total),
		count: 0,
		size: 0,
	}
}
