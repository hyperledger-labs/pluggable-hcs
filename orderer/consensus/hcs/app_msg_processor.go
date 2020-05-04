/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/golang/protobuf/proto"
	hb "github.com/hyperledger/fabric/orderer/consensus/hcs/proto"
	"github.com/hyperledger/fabric/protoutil"
	"reflect"
	"time"
)

type signer interface {
	Sign(message []byte) ([]byte, error)
	Verify(message, signature []byte) bool
}

type blockCipher interface {
	Encrypt(plaintext []byte) (iv, ciphertext []byte, err error)
	Decrypt(iv, ciphertext []byte) (plaintext []byte, err error)
}

type appMsgProcessor interface {
	Split(message []byte) ([]*hb.ApplicationMessageChunk, error)
	Reassemble(chunk *hb.ApplicationMessageChunk) ([]byte, error)
	IsPending() bool
	ExpireByAge(maxAge uint64) int
	ExpireByAppID(appID []byte) (int, error)
}

func newAppMsgProcessor(appID []byte, chunkSize int, signer signer, blockCipher blockCipher) (appMsgProcessor, error) {
	if chunkSize <= 0 {
		return nil, fmt.Errorf("invalid chunkSize - %d", chunkSize)
	}
	if appID == nil {
		return nil, fmt.Errorf("appID can't be nil")
	}
	if len(appID) == 0 {
		return nil, fmt.Errorf("appID can't be empty")
	}
	if isNil(signer) {
		return nil, fmt.Errorf("must provide signer")
	}
	return &appMsgProcessorImpl{
		appID:           appID,
		chunkSize:       chunkSize,
		signer:          signer,
		blockCipher:     blockCipher,
		noBlockCipher:   isNil(blockCipher),
		holders:         map[string]*chunkHolder{},
		holdersByAppID:  map[string]map[string]struct{}{},
		holderListByAge: list.New(),
	}, nil
}

type appMsgProcessorImpl struct {
	appID         []byte
	chunkSize     int
	signer        signer
	blockCipher   blockCipher
	noBlockCipher bool

	holders         map[string]*chunkHolder
	holdersByAppID  map[string]map[string]struct{} // holder keys organized by appID
	holderListByAge *list.List                     // from oldest to youngest
	tick            uint64
}

func (processor *appMsgProcessorImpl) Split(message []byte) ([]*hb.ApplicationMessageChunk, error) {
	if message == nil {
		return nil, fmt.Errorf("invalid parameter, message is nil")
	} else if len(message) == 0 {
		return nil, fmt.Errorf("invalid parameter, empty message")
	}

	// wrap message into ApplicationMessage
	appMsg := &hb.ApplicationMessage{
		ApplicationMessageId: &hb.ApplicationMessageID{
			ValidStart:    timestampProtoOrPanic(time.Now()),
			ApplicationID: processor.appID,
		},
		BusinessProcessMessage: message,
	}
	msgHash := sha256.Sum256(message)
	appMsg.UnencryptedBusinessProcessMessageHash = msgHash[:]
	signature, err := processor.signer.Sign(appMsg.UnencryptedBusinessProcessMessageHash)
	if err != nil {
		return nil, fmt.Errorf("failed to sign the unencrypted business process message hash - %v", err)
	}
	appMsg.BusinessProcessSignatureOnHash = signature
	if !processor.noBlockCipher {
		iv, ciphertext, err := processor.blockCipher.Encrypt(message)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt message - %v", err)
		}
		appMsg.BusinessProcessMessage = ciphertext
		appMsg.EncryptionRandom = iv
	}

	data := protoutil.MarshalOrPanic(appMsg)
	chunkSize := processor.chunkSize
	chunks := make([]*hb.ApplicationMessageChunk, (len(data)+chunkSize-1)/chunkSize)
	for i := 0; i < len(chunks); i++ {
		first := i * chunkSize
		last := (i + 1) * chunkSize
		if last > len(data) {
			last = len(data)
		}
		chunks[i] = &hb.ApplicationMessageChunk{
			ApplicationMessageId: appMsg.ApplicationMessageId,
			ChunksCount:          int32(len(chunks)),
			ChunkIndex:           int32(i),
			MessageChunk:         data[first:last],
		}
	}
	return chunks, nil
}

func (processor *appMsgProcessorImpl) Reassemble(chunk *hb.ApplicationMessageChunk) ([]byte, error) {
	isValidChunk := false
	var holder *chunkHolder
	defer func() {
		if isValidChunk {
			processor.tick++
			if holder != nil {
				holder.tick = processor.tick
			}
		}
	}()

	if chunk.ChunksCount < 1 {
		return nil, fmt.Errorf("invalid ChunksCount - %d, corrupted data", chunk.ChunksCount)
	}
	if chunk.ChunkIndex < 0 || chunk.ChunkIndex >= chunk.ChunksCount {
		return nil, fmt.Errorf("invalid ChunkIndex - %d, ChunksCount - %d, coruupted data", chunk.ChunkIndex, chunk.ChunksCount)
	}

	var protoMsg []byte
	if chunk.ChunksCount == 1 {
		// single-chunk message
		isValidChunk = true
		protoMsg = chunk.MessageChunk
	} else {
		var ok bool
		holderKey := makeHolderKey(*chunk.ApplicationMessageId)
		if holder, ok = processor.holders[holderKey]; !ok {
			holder = newChunkHolder(chunk.ChunksCount, holderKey)
			processor.holders[holderKey] = holder
			appIDKey := hex.EncodeToString(processor.appID)
			appHolders, ok := processor.holdersByAppID[appIDKey]
			if !ok {
				appHolders = map[string]struct{}{}
				processor.holdersByAppID[appIDKey] = appHolders
			}
			appHolders[holderKey] = struct{}{}
		}

		index := chunk.ChunkIndex
		if holder.chunks[index] != nil {
			return nil, fmt.Errorf("received duplicate chunk at index %d", index)
		}
		if chunk.ChunksCount != int32(len(holder.chunks)) {
			return nil, fmt.Errorf("incorrect chunk ChunksCount = %d, corrupted data", chunk.ChunksCount)
		}
		isValidChunk = true
		holder.chunks[index] = chunk
		holder.appID = chunk.ApplicationMessageId.ApplicationID
		holder.count++
		holder.size += uint32(len(chunk.MessageChunk))
		if holder.count != int32(len(holder.chunks)) {
			// the holder becomes the youngest
			if holder.el != nil {
				processor.holderListByAge.MoveToBack(holder.el)
			} else {
				holder.el = processor.holderListByAge.PushBack(holder)
			}
			return nil, nil
		} else {
			reassembled := make([]byte, 0, holder.size)
			for _, f := range holder.chunks {
				reassembled = append(reassembled, f.MessageChunk...)
			}
			processor.removeHolder(holder, true)
			protoMsg = reassembled
		}
	}

	appMsg := &hb.ApplicationMessage{}
	if err := proto.Unmarshal(protoMsg, appMsg); err != nil {
		return nil, err
	}
	plaintext := appMsg.BusinessProcessMessage
	if appMsg.EncryptionRandom != nil {
		if processor.noBlockCipher {
			return nil, fmt.Errorf("application message is encrypted but appMsgProcessor is configured without a blockciper")
		}
		decrypted, err := processor.blockCipher.Decrypt(appMsg.EncryptionRandom, appMsg.BusinessProcessMessage)
		if err != nil {
			return nil, fmt.Errorf("failed  to decrypt business process message - %v", err)
		}
		plaintext = decrypted
	}
	msgHash := sha256.Sum256(plaintext)
	if !bytes.Equal(msgHash[:], appMsg.UnencryptedBusinessProcessMessageHash) {
		return nil, fmt.Errorf("hash on unecnrypted business process message does not match, corrupted or malicious data")
	}
	if !processor.signer.Verify(appMsg.UnencryptedBusinessProcessMessageHash, appMsg.BusinessProcessSignatureOnHash) {
		return nil, fmt.Errorf("failed to verify unencrypted business process message signature")
	}
	return plaintext, nil
}

func (processor *appMsgProcessorImpl) IsPending() bool {
	return len(processor.holders) != 0
}

func (processor *appMsgProcessorImpl) ExpireByAge(maxAge uint64) (count int) {
	for {
		oldest := processor.holderListByAge.Front()
		if oldest == nil {
			break
		}
		holder := oldest.Value.(*chunkHolder)
		age := calcAge(holder.tick, processor.tick)
		if age >= maxAge {
			processor.removeHolder(holder, true)
			count++
		} else {
			break
		}
	}
	return
}

func (processor *appMsgProcessorImpl) ExpireByAppID(appID []byte) (int, error) {
	if appID == nil || len(appID) == 0 {
		return 0, fmt.Errorf("invalid appID - %v", appID)
	}
	key := hex.EncodeToString(appID)
	count := 0
	if appHolders := processor.holdersByAppID[key]; appHolders != nil {
		delete(processor.holdersByAppID, key)
		count = len(appHolders)
		for holderKey := range appHolders {
			if holder := processor.holders[holderKey]; holder != nil {
				processor.removeHolder(holder, false)
			}
		}
	}
	return count, nil
}

func (processor *appMsgProcessorImpl) removeHolder(holder *chunkHolder, removeFomAppHolders bool) {
	delete(processor.holders, holder.key)
	if holder.el != nil {
		processor.holderListByAge.Remove(holder.el)
	}
	if removeFomAppHolders {
		if appHolders := processor.holdersByAppID[hex.EncodeToString(holder.appID)]; appHolders != nil {
			delete(appHolders, holder.key)
		}
	}
}

type chunkHolder struct {
	chunks []*hb.ApplicationMessageChunk
	appID  []byte
	key    string
	count  int32
	size   uint32
	tick   uint64
	el     *list.Element
}

func newChunkHolder(total int32, key string) *chunkHolder {
	return &chunkHolder{chunks: make([]*hb.ApplicationMessageChunk, total), key: key}
}

// helper functions

func makeHolderKey(id hb.ApplicationMessageID) string {
	return fmt.Sprintf("%s%s", hex.EncodeToString(id.ApplicationID), id.ValidStart)
}

func calcAge(bornTick uint64, currentTick uint64) uint64 {
	if currentTick < bornTick {
		return ^uint64(1) - bornTick + currentTick + 1
	}
	return currentTick - bornTick
}

func isNil(i interface{}) bool {
	return i == nil || reflect.ValueOf(i).IsNil()
}
