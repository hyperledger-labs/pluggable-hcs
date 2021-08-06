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
	"reflect"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/hashgraph/hedera-sdk-go/v2"
	hb "github.com/hyperledger/fabric/orderer/consensus/hcs/protodef"
	"github.com/hyperledger/fabric/protoutil"
)

type signer interface {
	Sign(message []byte) (publicKey []byte, signature []byte, err error)
	Verify(message, signerPublicKey, signature []byte) bool
}

type appMsgProcessor interface {
	Split(message []byte, validStart time.Time) ([]*hb.ApplicationMessageChunk, []byte, error)
	Reassemble(chunk *hb.ApplicationMessageChunk, timestamp time.Time) ([]byte, []byte, int, int, error)
	IsPending() bool
	ExpireByAppID(appID []byte) (int, int, error)
}

func newAppMsgProcessor(accountID hedera.AccountID, appID []byte, chunkSize int, reassembleTimeout time.Duration, signer signer) (appMsgProcessor, error) {
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
		accountID:         accountID,
		appID:             appID,
		chunkSize:         chunkSize,
		reassembleTimeout: reassembleTimeout,
		signer:            signer,
		holders:           map[string]*chunkHolder{},
		holdersByAppID:    map[string]map[string]struct{}{},
		holderListByAge:   list.New(),
	}, nil
}

type appMsgProcessorImpl struct {
	accountID         hedera.AccountID
	appID             []byte
	chunkSize         int
	reassembleTimeout time.Duration
	signer            signer

	holders         map[string]*chunkHolder
	holdersByAppID  map[string]map[string]struct{} // holder keys organized by appID
	holderListByAge *list.List                     // from oldest to youngest
}

func (processor *appMsgProcessorImpl) Split(message []byte, validStart time.Time) ([]*hb.ApplicationMessageChunk, []byte, error) {
	if message == nil {
		return nil, nil, fmt.Errorf("invalid parameter, message is nil")
	} else if len(message) == 0 {
		return nil, nil, fmt.Errorf("invalid parameter, empty message")
	}

	// wrap message into ApplicationMessage
	appMsg := &hb.ApplicationMessage{
		ApplicationMessageId: &hb.ApplicationMessageID{
			ValidStart: timestampProtoOrPanic(validStart),
			AccountID: &hb.AccountID{
				ShardNum:   int64(processor.accountID.Shard),
				RealmNum:   int64(processor.accountID.Realm),
				AccountNum: int64(processor.accountID.Account),
			},
			Metadata: &any.Any{
				Value: processor.appID,
			},
		},
		BusinessProcessMessage: message,
	}
	msgHash := sha256.Sum256(message)
	appMsg.UnencryptedBusinessProcessMessageHash = msgHash[:]
	publicKey, signature, err := processor.signer.Sign(appMsg.UnencryptedBusinessProcessMessageHash)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to sign the unencrypted business process message hash - %v", err)
	}
	appSig := &hb.ApplicationSignature{
		PublicKey: publicKey,
		Signature: signature,
	}
	appMsg.BusinessProcessSignatureOnHash = protoutil.MarshalOrPanic(appSig)

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
	return chunks, appMsg.UnencryptedBusinessProcessMessageHash, nil
}

func (processor *appMsgProcessorImpl) Reassemble(chunk *hb.ApplicationMessageChunk, timestamp time.Time) (
	message []byte,
	hash []byte,
	expiredMessages int,
	expiredChunks int,
	err error,
) {
	isValidChunk := false
	var holder *chunkHolder
	defer func() {
		if isValidChunk {
			if holder != nil {
				holder.timestamp = timestamp
			}

			expiredMessages, expiredChunks = processor.expireByTimestamp(timestamp)
		}
	}()

	if chunk.ChunksCount < 1 {
		err = fmt.Errorf("invalid ChunksCount - %d, corrupted data", chunk.ChunksCount)
		return
	}
	if chunk.ChunkIndex < 0 || chunk.ChunkIndex >= chunk.ChunksCount {
		err = fmt.Errorf("invalid ChunkIndex - %d, ChunksCount - %d, coruppted data", chunk.ChunkIndex, chunk.ChunksCount)
		return
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
			holder = newChunkHolder(chunk.ChunksCount, holderKey, chunk.ApplicationMessageId.Metadata.Value)
			processor.holders[holderKey] = holder
			appIDKey := hex.EncodeToString(holder.appID)
			appHolders, ok := processor.holdersByAppID[appIDKey]
			if !ok {
				appHolders = map[string]struct{}{}
				processor.holdersByAppID[appIDKey] = appHolders
			}
			appHolders[holderKey] = struct{}{}
		}

		index := chunk.ChunkIndex
		if holder.chunks[index] != nil {
			err = fmt.Errorf("received duplicate chunk at index %d", index)
			return
		}
		if chunk.ChunksCount != int32(len(holder.chunks)) {
			err = fmt.Errorf("incorrect chunk ChunksCount = %d, corrupted data", chunk.ChunksCount)
			return
		}
		isValidChunk = true
		holder.chunks[index] = chunk
		holder.received++
		holder.size += uint32(len(chunk.MessageChunk))
		if holder.received != int32(len(holder.chunks)) {
			// the holder becomes the youngest
			if holder.el != nil {
				processor.holderListByAge.MoveToBack(holder.el)
			} else {
				holder.el = processor.holderListByAge.PushBack(holder)
			}
			return
		}

		reassembled := make([]byte, 0, holder.size)
		for _, f := range holder.chunks {
			reassembled = append(reassembled, f.MessageChunk...)
		}
		processor.removeHolder(holder, true)
		protoMsg = reassembled
	}

	appMsg := &hb.ApplicationMessage{}
	if err = proto.Unmarshal(protoMsg, appMsg); err != nil {
		return
	}
	plaintext := appMsg.BusinessProcessMessage
	if appMsg.EncryptionRandom != nil {
		err = fmt.Errorf("encrypted payload support is deprecated")
		return
	}

	msgHash := sha256.Sum256(plaintext)
	if !bytes.Equal(msgHash[:], appMsg.UnencryptedBusinessProcessMessageHash) {
		err = fmt.Errorf("hash on unecnrypted business process message does not match, corrupted or malicious data")
		return
	}
	appSig := &hb.ApplicationSignature{}
	if err = proto.Unmarshal(appMsg.BusinessProcessSignatureOnHash, appSig); err != nil {
		err = fmt.Errorf("failed to unmarshal BusinessProcessSignatureOnHash, corrupted data = %v", err)
		return
	}
	if !processor.signer.Verify(appMsg.UnencryptedBusinessProcessMessageHash, appSig.PublicKey, appSig.Signature) {
		err = fmt.Errorf("failed to verify unencrypted business process message signature")
		return
	}
	message = plaintext
	hash = appMsg.UnencryptedBusinessProcessMessageHash
	return
}

func (processor *appMsgProcessorImpl) IsPending() bool {
	return len(processor.holders) != 0
}

func (processor *appMsgProcessorImpl) expireByTimestamp(now time.Time) (expiredMessages int, expiredChunks int) {
	for {
		oldest := processor.holderListByAge.Front()
		if oldest == nil {
			break
		}
		holder := oldest.Value.(*chunkHolder)
		diff := now.Sub(holder.timestamp)
		if diff >= processor.reassembleTimeout {
			expiredMessages++
			expiredChunks += int(holder.received)
			processor.removeHolder(holder, true)
		} else {
			break
		}
	}

	return
}

func (processor *appMsgProcessorImpl) ExpireByAppID(appID []byte) (expiredMessages int, expiredChunks int, err error) {
	if appID == nil || len(appID) == 0 {
		err = fmt.Errorf("invalid appID - %v", appID)
		return
	}
	key := hex.EncodeToString(appID)
	if appHolders := processor.holdersByAppID[key]; appHolders != nil {
		delete(processor.holdersByAppID, key)
		for holderKey := range appHolders {
			if holder := processor.holders[holderKey]; holder != nil {
				expiredMessages++
				expiredChunks += int(holder.received)
				processor.removeHolder(holder, false)
			}
		}
	}
	return
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
	chunks    []*hb.ApplicationMessageChunk
	appID     []byte
	key       string
	received  int32
	size      uint32
	timestamp time.Time
	el        *list.Element
}

func newChunkHolder(total int32, key string, appID []byte) *chunkHolder {
	return &chunkHolder{
		chunks: make([]*hb.ApplicationMessageChunk, total),
		key:    key,
		appID:  appID,
	}
}

// helper functions

func makeHolderKey(id hb.ApplicationMessageID) string {
	return fmt.Sprintf("%s:%ds%d", hex.EncodeToString(id.Metadata.Value), id.ValidStart.Seconds, id.ValidStart.Nanos)
}

func isNil(i interface{}) bool {
	return i == nil || reflect.ValueOf(i).IsNil()
}
