/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/any"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/hashgraph/hedera-sdk-go"

	"github.com/golang/protobuf/proto"
	mockhcs "github.com/hyperledger/fabric/orderer/consensus/hcs/mock"
	hb "github.com/hyperledger/fabric/orderer/consensus/hcs/protodef"
	"github.com/stretchr/testify/assert"
)

//go:generate counterfeiter -o mock/signer.go --fake-name Signer . mockSigner
type mockSigner interface {
	signer
}

const (
	normalReassembleTimeout = 30 * time.Second
	shortReassembleTimeout  = 5 * time.Second
)

var (
	testAccountID = hedera.AccountID{
		Shard:   0,
		Realm:   0,
		Account: 160,
	}
)

func TestNewEmptyAppMsgProcessor(t *testing.T) {
	type args struct {
		appID     []byte
		chunkSize int
		signer    signer
	}
	var tests = []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Proper",
			args: args{
				appID:     []byte("sample app id"),
				chunkSize: maxConsensusMessageSize,
				signer:    &mockhcs.Signer{},
			},
			wantErr: false,
		},
		{
			name: "ProperWithNilSignerNilBlockCipher",
			args: args{
				appID:     []byte("sample app id"),
				chunkSize: maxConsensusMessageSize,
				signer:    &mockhcs.Signer{},
			},
			wantErr: false,
		},
		{
			name: "WithNilAppID",
			args: args{
				appID:     nil,
				chunkSize: maxConsensusMessageSize,
				signer:    &mockhcs.Signer{},
			},
			wantErr: true,
		},
		{
			name: "WithEmptyAppID",
			args: args{
				appID:     []byte{},
				chunkSize: maxConsensusMessageSize,
				signer:    &mockhcs.Signer{},
			},
			wantErr: true,
		},
		{
			name: "WithZeroChunkSize",
			args: args{
				appID:     []byte("sample app id"),
				chunkSize: 0,
				signer:    &mockhcs.Signer{},
			},
			wantErr: true,
		},
		{
			name: "WithNegativeChunkSize",
			args: args{
				appID:     []byte("sample app id"),
				chunkSize: -10,
				signer:    &mockhcs.Signer{},
			},
			wantErr: true,
		},
		{
			name: "WithNilSigner",
			args: args{
				appID:     []byte("sample app id"),
				chunkSize: maxConsensusMessageSize,
				signer:    nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amp, err := newAppMsgProcessor(testAccountID, tt.args.appID, tt.args.chunkSize, normalReassembleTimeout, tt.args.signer)
			if !tt.wantErr {
				assert.NoError(t, err, "Expected newAppMsgProcessor returns no error")
				assert.NotNil(t, amp, "Expected newAppMsgProcessor returns non-nil value")
			} else {
				assert.Error(t, err, "Expected newAppMsgProcessor returns error")
				assert.Nil(t, amp, "Expected newAppMsgProcessor returns nil value")
			}
		})
	}
}

func TestAppMsgProcessor(t *testing.T) {
	fakeAppID := []byte("sample app id")
	fakeOtherAppID := []byte("sample other app id")

	t.Run("TestSplit", func(t *testing.T) {
		fakeSigner := &mockhcs.Signer{}
		fakeSigner.SignReturns([]byte("fake public key"), []byte("fake signature"), nil)
		badSigner := &mockhcs.Signer{}
		badSigner.SignReturns(nil, nil, fmt.Errorf("can't sign message"))

		var tests = []struct {
			name           string
			signer         *mockhcs.Signer
			message        []byte
			wantErr        bool
			wantChunkCount int
		}{
			{
				name:           "ProperOneChunk",
				signer:         fakeSigner,
				message:        make([]byte, maxConsensusMessageSize-400),
				wantErr:        false,
				wantChunkCount: 1,
			},
			{
				name:           "ProperMultipleChunks",
				signer:         fakeSigner,
				message:        make([]byte, maxConsensusMessageSize*5),
				wantErr:        false,
				wantChunkCount: 6,
			},
			{
				name:    "WithNilMessage",
				signer:  fakeSigner,
				message: nil,
				wantErr: true,
			},
			{
				name:    "WithEmptyMessage",
				signer:  fakeSigner,
				message: []byte{},
				wantErr: true,
			},
			{
				name:    "WithSignerError",
				signer:  badSigner,
				message: make([]byte, maxConsensusMessageSize*5),
				wantErr: true,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				amp, err := newAppMsgProcessor(testAccountID, fakeAppID, maxConsensusMessageSize, normalReassembleTimeout, tt.signer)
				assert.NotNil(t, amp, "Expected newAppMsgProcessor return non-nil value")
				assert.NoError(t, err, "Expected newAppMsgProcessor return no err")

				prevSignCallCount := tt.signer.SignCallCount()

				chunks, _, err := amp.Split(tt.message, time.Now())
				if tt.wantErr {
					assert.Error(t, err, "Expected Split return err")
				} else {
					assert.NotNil(t, chunks, "Expected Split return no-nil value")
					assert.NoError(t, err, "Expected Split return no error")

					assert.Equal(t, tt.wantChunkCount, len(chunks), "Expected Split return the correct number of chunks")
					for index, chunk := range chunks {
						if index != len(chunks)-1 {
							assert.Equal(t, maxConsensusMessageSize, len(chunk.MessageChunk), "Expected a full chunk")
						}
						assert.Equal(t, fakeAppID, chunk.ApplicationMessageId.Metadata.Value, "Expected correct appID")
						assert.Equal(t, tt.wantChunkCount, int(chunk.ChunksCount), "Expected correct ChunksCount")
						assert.Equal(t, index, int(chunk.ChunkIndex), "Expected correct ChunkIndex")
						assert.Equal(t, 1, tt.signer.SignCallCount()-prevSignCallCount, "Expected Sign called one time")
					}
				}
			})
		}
	})

	t.Run("TestReassemble", func(t *testing.T) {
		fakeSigner := &mockhcs.Signer{}
		fakePublicKey := []byte("sample public key")
		fakeSignature := []byte("sample signature")
		fakeSigner.SignStub = func([]byte) ([]byte, []byte, error) {
			signatureCopy := make([]byte, len(fakeSignature))
			copy(signatureCopy, fakeSignature)
			return fakePublicKey, signatureCopy, nil
		}
		fakeSigner.VerifyReturns(true)

		badSigner := &mockhcs.Signer{}
		badSigner.SignStub = fakeSigner.SignStub
		badSigner.VerifyReturns(false)

		var tests = []struct {
			name                string
			signer              *mockhcs.Signer
			messageSize         int
			expectedChunksCount int32
			chunksModifyFunc    func(t *testing.T, chunks []*hb.ApplicationMessageChunk)
			wantErr             bool
		}{
			{
				name:                "ProperOneChunk",
				signer:              fakeSigner,
				messageSize:         maxConsensusMessageSize - 400,
				expectedChunksCount: 1,
				wantErr:             false,
			},
			{
				name:                "ProperMultipleChunks",
				signer:              fakeSigner,
				messageSize:         maxConsensusMessageSize * 5,
				expectedChunksCount: 6,
				wantErr:             false,
			},
			{
				name:                "ProperMultipleChunksOutOfOrder",
				signer:              fakeSigner,
				messageSize:         maxConsensusMessageSize * 5,
				expectedChunksCount: 6,
				chunksModifyFunc: func(t *testing.T, chunks []*hb.ApplicationMessageChunk) {
					// shuffle it
					rand.Shuffle(len(chunks), func(i, j int) {
						chunks[i], chunks[j] = chunks[j], chunks[i]
					})
				},
				wantErr: false,
			},
			{
				name:                "WithSignerVerifyFailed",
				signer:              badSigner,
				messageSize:         maxConsensusMessageSize * 5,
				expectedChunksCount: 6,
				wantErr:             true,
			},
			{
				name:                "WithOutOfBoundChunk",
				signer:              fakeSigner,
				messageSize:         maxConsensusMessageSize * 5,
				expectedChunksCount: 6,
				chunksModifyFunc: func(t *testing.T, chunks []*hb.ApplicationMessageChunk) {
					chunks[0].ChunkIndex = 6
				},
				wantErr: true,
			},
			{
				name:                "WithInvalidChunksCount",
				signer:              fakeSigner,
				messageSize:         maxConsensusMessageSize - 400,
				expectedChunksCount: 1,
				chunksModifyFunc: func(t *testing.T, chunks []*hb.ApplicationMessageChunk) {
					chunks[0].ChunksCount = 0
				},
				wantErr: true,
			},
			{
				name:                "WithNegativeChunkIndex",
				signer:              fakeSigner,
				messageSize:         maxConsensusMessageSize - 400,
				expectedChunksCount: 1,
				chunksModifyFunc: func(t *testing.T, chunks []*hb.ApplicationMessageChunk) {
					chunks[0].ChunkIndex = -1
				},
				wantErr: true,
			},
			{
				name:                "WithDuplicateChunk",
				signer:              fakeSigner,
				messageSize:         maxConsensusMessageSize * 5,
				expectedChunksCount: 6,
				chunksModifyFunc: func(t *testing.T, chunks []*hb.ApplicationMessageChunk) {
					chunks[1] = chunks[0]
				},
				wantErr: true,
			},
			{
				name:                "WithCorruptedData",
				signer:              fakeSigner,
				messageSize:         maxConsensusMessageSize * 5,
				expectedChunksCount: 6,
				chunksModifyFunc: func(t *testing.T, chunks []*hb.ApplicationMessageChunk) {
					messageChunk := chunks[0].MessageChunk
					for i := 0; i < len(messageChunk); i++ {
						messageChunk[i] = ^messageChunk[i]
					}
				},
				wantErr: true,
			},
			{
				name:                "WithIncorrectChunksCount",
				signer:              fakeSigner,
				messageSize:         maxConsensusMessageSize * 5,
				expectedChunksCount: 6,
				chunksModifyFunc: func(t *testing.T, chunks []*hb.ApplicationMessageChunk) {
					chunks[0].ChunksCount++
				},
				wantErr: true,
			},
			{
				name:                "WithCorruptedBusinessMessage",
				signer:              fakeSigner,
				messageSize:         maxConsensusMessageSize - 400,
				expectedChunksCount: 1,
				chunksModifyFunc: func(t *testing.T, chunks []*hb.ApplicationMessageChunk) {
					msg := &hb.ApplicationMessage{}
					assert.NoError(t, proto.Unmarshal(chunks[0].MessageChunk, msg), "Expected proto unmarshal successful")
					msg.BusinessProcessMessage[0] = ^msg.BusinessProcessMessage[0]
					var err error
					chunks[0].MessageChunk, err = proto.Marshal(msg)
					assert.NoError(t, err, "Expected proto marshal successful")
				},
				wantErr: true,
			},
			{
				name:                "WithCorruptedSignature",
				signer:              fakeSigner,
				messageSize:         maxConsensusMessageSize - 400,
				expectedChunksCount: 1,
				chunksModifyFunc: func(t *testing.T, chunks []*hb.ApplicationMessageChunk) {
					msg := &hb.ApplicationMessage{}
					assert.NoError(t, proto.Unmarshal(chunks[0].MessageChunk, msg), "Expected proto unmarshal successful")
					for i := 0; i < len(msg.BusinessProcessSignatureOnHash); i++ {
						msg.BusinessProcessSignatureOnHash[i] = ^msg.BusinessProcessSignatureOnHash[i]
					}
					var err error
					chunks[0].MessageChunk, err = proto.Marshal(msg)
					assert.NoError(t, err, "Expected proto marshal successful")
				},
				wantErr: true,
			},
			{
				name:                "WithEncryptedDataAndNoBlockCipher",
				signer:              fakeSigner,
				messageSize:         maxConsensusMessageSize - 400,
				expectedChunksCount: 1,
				chunksModifyFunc: func(t *testing.T, chunks []*hb.ApplicationMessageChunk) {
					msg := &hb.ApplicationMessage{}
					assert.NoError(t, proto.Unmarshal(chunks[0].MessageChunk, msg), "Expected proto unmarshal successful")
					msg.EncryptionRandom = []byte("iv")
					var err error
					chunks[0].MessageChunk, err = proto.Marshal(msg)
					assert.NoError(t, err, "Expected proto marshal successful")
				},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				amp, err := newAppMsgProcessor(testAccountID, fakeAppID, maxConsensusMessageSize, normalReassembleTimeout, tt.signer)
				assert.NotNil(t, amp, "Expected newAppMsgProcessor return non-nil value")
				assert.NoError(t, err, "Expected newAppMsgProcessor return no error")

				message := make([]byte, tt.messageSize)
				rand.Read(message)
				chunks, _, err := amp.Split(message, time.Now())
				assert.NotNil(t, chunks, "Expected Split return non-nil chunks")
				assert.Equal(t, tt.expectedChunksCount, int32(len(chunks)), "Expected Split return correct number of chunks")
				assert.NoError(t, err, "Expected Split return no error")
				if tt.chunksModifyFunc != nil {
					tt.chunksModifyFunc(t, chunks)
				}

				prevVerifyCallCount := tt.signer.VerifyCallCount()

				var reassembled []byte
				for index, chunk := range chunks {
					reassembled, _, _, _, err = amp.Reassemble(chunk, time.Now())
					if index != len(chunks)-1 {
						assert.Nil(t, reassembled, "Expected Reassemble returns nil value for all but last chunk")
					}
					if err != nil {
						break
					}
				}
				if tt.wantErr {
					assert.Nil(t, reassembled, "Expected Reassemble returns nil value")
					assert.Error(t, err, "Expected Reassemble returns error")
				} else {
					assert.NotNil(t, reassembled, "Expected Reassemble return non-nil value")
					assert.Equal(t, message, reassembled, "Expected chunks reassembled to match original messsage")
					assert.NoError(t, err, "Expected Reassemble returns no error")

					assert.Equal(t, 1, tt.signer.VerifyCallCount()-prevVerifyCallCount, "Expected Verify called one time")
					signData := tt.signer.SignArgsForCall(tt.signer.SignCallCount() - 1)
					verifyData, verifyPublicKey, verifySignature := tt.signer.VerifyArgsForCall(prevVerifyCallCount)
					assert.Equal(t, signData, verifyData, "Expected signData and verifyData are the same")
					assert.Equal(t, fakePublicKey, verifyPublicKey, "Expected public keys match")
					assert.Equal(t, fakeSignature, verifySignature, "Expected signatures match")
				}
			})
		}
	})

	t.Run("TestIsPending", func(t *testing.T) {
		mockSigner := &mockhcs.Signer{}
		mockSigner.SignReturns([]byte("public key"), []byte("signature"), nil)
		mockSigner.VerifyReturns(true)

		t.Run("ProperEmpty", func(t *testing.T) {
			amp, err := newAppMsgProcessor(testAccountID, fakeAppID, maxConsensusMessageSize, normalReassembleTimeout, mockSigner)
			assert.NotNil(t, amp, "Expected newAppMsgProcessor returns non-nil value")
			assert.NoError(t, err, "Expected newAppMsgProcessor returns no error")
			assert.False(t, amp.IsPending(), "Expected new amp IsPending = false")
		})

		t.Run("ProperWithData", func(t *testing.T) {
			amp, err := newAppMsgProcessor(testAccountID, fakeAppID, maxConsensusMessageSize, normalReassembleTimeout, mockSigner)
			assert.NotNil(t, amp, "Expected newAppMsgProcessor returns non-nil value")
			assert.NoError(t, err, "Expected newAppMsgProcessor returns no error")

			chunks, _, err := amp.Split(make([]byte, maxConsensusMessageSize+100), time.Now())
			assert.NotNil(t, chunks, "Expected Split returns non-nil value")
			assert.True(t, len(chunks) > 1, "Expected more than one chunks")
			assert.NoError(t, err, "Expected Split returns no error")
			assert.False(t, amp.IsPending(), "Expected new amp IsPending = false")

			_, _, _, _, err = amp.Reassemble(chunks[0], time.Now())
			assert.NoError(t, err, "Expected Reassemble returns no error")
			assert.True(t, amp.IsPending(), "Expected IsPending = true")

			for i := 1; i < len(chunks); i++ {
				_, _, _, _, err = amp.Reassemble(chunks[i], time.Now())
				assert.NoError(t, err, "Expected Reassemble returns no error")
			}
			assert.False(t, amp.IsPending(), "Expected IsPending = false")
		})
	})

	t.Run("TestExpireByTimestamp", func(t *testing.T) {
		fakesSigner := &mockhcs.Signer{}
		fakesSigner.SignReturns([]byte("fake public key"), []byte("fake signature"), nil)
		fakesSigner.VerifyReturns(true)

		amp, err := newAppMsgProcessor(testAccountID, fakeAppID, maxConsensusMessageSize, shortReassembleTimeout, fakesSigner)
		assert.NotNil(t, amp, "Expected newAppMsgProcessor return non-nil value")
		assert.NoError(t, err, "Expected newAppMsgProcessor return no error")

		chunks1, _, err := amp.Split(make([]byte, 2*maxConsensusMessageSize), time.Now())
		assert.True(t, chunks1 != nil && len(chunks1) > 2, "Expected Split returns more than one chunks")
		assert.NoError(t, err, "Expected Split returns no error")

		chunks2, _, err := amp.Split(make([]byte, 2*maxConsensusMessageSize), time.Now())
		assert.True(t, chunks2 != nil && len(chunks2) > 2, "Expected Split returns more than one chunks")
		assert.NoError(t, err, "Expected Split returns no error")

		baseTimestamp := time.Now()

		_, _, expiredMessages, expiredChunks, err := amp.Reassemble(chunks1[0], baseTimestamp)
		assert.NoError(t, err, "Expected Reassemble returns no error")
		assert.Equal(t, 0, expiredMessages, "Expected no messages expired")
		assert.Equal(t, 0, expiredChunks, "Expected no chunks expired")

		nextTimestamp := baseTimestamp.Add(shortReassembleTimeout / 2)
		_, _, expiredMessages, expiredChunks, err = amp.Reassemble(chunks2[0], nextTimestamp)
		assert.NoError(t, err, "Expected Reassemble returns no error")
		assert.Equal(t, 0, expiredMessages, "Expected no messages expired")
		assert.Equal(t, 0, expiredChunks, "Expected no chunks expired")

		nextTimestamp = nextTimestamp.Add(shortReassembleTimeout)
		_, _, expiredMessages, expiredChunks, err = amp.Reassemble(chunks1[1], nextTimestamp)
		assert.Equal(t, 1, expiredMessages, "Expected a message has expired")
		assert.Equal(t, 1, expiredChunks, "Expected a chunk has expired")
	})

	t.Run("TestExpireByAppID", func(t *testing.T) {
		fakesSigner := &mockhcs.Signer{}
		fakesSigner.SignReturns([]byte("fake public key"), []byte("fake signature"), nil)
		fakesSigner.VerifyReturns(true)

		var tests = []struct {
			name                 string
			createChunksFunc     func(t *testing.T, amp appMsgProcessor) []*hb.ApplicationMessageChunk
			appIDArg             []byte
			wantErr              bool
			expectedMessageCount int
			expectedChunkCount   int
		}{
			{
				name: "ProperExpireNothing",
				createChunksFunc: func(t *testing.T, amp appMsgProcessor) []*hb.ApplicationMessageChunk {
					return []*hb.ApplicationMessageChunk{}
				},
				appIDArg:             fakeAppID,
				wantErr:              false,
				expectedMessageCount: 0,
				expectedChunkCount:   0,
			},
			{
				name: "ProperExpireNothingAfterFullReassemble",
				createChunksFunc: func(t *testing.T, amp appMsgProcessor) []*hb.ApplicationMessageChunk {
					chunks, _, err := amp.Split(make([]byte, maxConsensusMessageSize*2), time.Now())
					assert.True(t, chunks != nil && len(chunks) > 2, "Expected Split returns chunks")
					assert.NoError(t, err, "Expected Split returns no error")
					return chunks
				},
				appIDArg:             fakeAppID,
				wantErr:              false,
				expectedMessageCount: 0,
				expectedChunkCount:   0,
			},
			{
				name: "ProperExpireWithPartialReassemble",
				createChunksFunc: func(t *testing.T, amp appMsgProcessor) []*hb.ApplicationMessageChunk {
					chunks, _, err := amp.Split(make([]byte, maxConsensusMessageSize*2), time.Now())
					assert.True(t, chunks != nil && len(chunks) > 2, "Expected Split returns chunks")
					assert.NoError(t, err, "Expected Split returns no error")
					return chunks[0:1]
				},
				appIDArg:             fakeAppID,
				wantErr:              false,
				expectedMessageCount: 1,
				expectedChunkCount:   1,
			},
			{
				name: "WithNilAppIDArg",
				createChunksFunc: func(t *testing.T, amp appMsgProcessor) []*hb.ApplicationMessageChunk {
					return []*hb.ApplicationMessageChunk{}
				},
				appIDArg: nil,
				wantErr:  true,
			},
			{
				name: "WithEmptyAppIDArg",
				createChunksFunc: func(t *testing.T, amp appMsgProcessor) []*hb.ApplicationMessageChunk {
					return []*hb.ApplicationMessageChunk{}
				},
				appIDArg: []byte{},
				wantErr:  true,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				amp, err := newAppMsgProcessor(testAccountID, fakeAppID, maxConsensusMessageSize, normalReassembleTimeout, fakesSigner)
				assert.NotNil(t, amp, "Expected newAppMsgProcessor return non-nil value")
				assert.NoError(t, err, "Expected newAppMsgProcessor return no error")

				chunks := tt.createChunksFunc(t, amp)
				for _, chunk := range chunks {
					_, _, _, _, err := amp.Reassemble(chunk, time.Now())
					assert.NoError(t, err, "Expected Reassemble returns no error")
				}
				expiredMessages, expiredChunks, err := amp.ExpireByAppID(tt.appIDArg)
				if tt.wantErr {
					assert.Error(t, err, "Expected ExpireByAppID return error")
				} else {
					assert.Equal(t, tt.expectedMessageCount, expiredMessages, "Expected ExpireByAppID returns correct message count")
					assert.Equal(t, tt.expectedChunkCount, expiredChunks, "Expected ExpireByAppID returns correct chunk count")
					assert.NoError(t, err, "Expected ExpireByAppID return error")
				}
			})
		}
	})

	t.Run("TestExpireByOtherAppID", func(t *testing.T) {
		fakesSigner := &mockhcs.Signer{}
		fakesSigner.SignReturns([]byte("fake public key"), []byte("fake signature"), nil)
		fakesSigner.VerifyReturns(true)

		var tests = []struct {
			name             string
			createChunksFunc func(t *testing.T, amp appMsgProcessor) []*hb.ApplicationMessageChunk
			appIDArg         []byte
			expectedCount    int
		}{
			{
				name: "ProperExpireNothing",
				createChunksFunc: func(t *testing.T, otherAmp appMsgProcessor) []*hb.ApplicationMessageChunk {
					return []*hb.ApplicationMessageChunk{}
				},
				appIDArg:      fakeOtherAppID,
				expectedCount: 0,
			},
			{
				name: "ProperExpireNothingAfterFullReassemble",
				createChunksFunc: func(t *testing.T, otherAmp appMsgProcessor) []*hb.ApplicationMessageChunk {
					chunks, _, err := otherAmp.Split(make([]byte, maxConsensusMessageSize*2), time.Now())
					assert.True(t, chunks != nil && len(chunks) > 2, "Expected Split returns chunks")
					assert.NoError(t, err, "Expected Split returns no error")
					return chunks
				},
				appIDArg:      fakeOtherAppID,
				expectedCount: 0,
			},
			{
				name: "ProperExpireWithPartialReassemble",
				createChunksFunc: func(t *testing.T, otherAmp appMsgProcessor) []*hb.ApplicationMessageChunk {
					chunks, _, err := otherAmp.Split(make([]byte, maxConsensusMessageSize*2), time.Now())
					assert.True(t, chunks != nil && len(chunks) > 2, "Expected Split returns chunks")
					assert.NoError(t, err, "Expected Split returns no error")
					return chunks[0:1]
				},
				appIDArg:      fakeOtherAppID,
				expectedCount: 1,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				amp, err := newAppMsgProcessor(testAccountID, fakeAppID, maxConsensusMessageSize, normalReassembleTimeout, fakesSigner)
				assert.NotNil(t, amp, "Expected newAppMsgProcessor return non-nil value")
				assert.NoError(t, err, "Expected newAppMsgProcessor return no error")

				otherAmp, err := newAppMsgProcessor(testAccountID, fakeOtherAppID, maxConsensusMessageSize, normalReassembleTimeout, fakesSigner)
				assert.NotNil(t, otherAmp, "Expected newAppMsgProcessor return non-nil value")
				assert.NoError(t, err, "Expected newAppMsgProcessor return no error")

				chunks := tt.createChunksFunc(t, otherAmp)
				for _, chunk := range chunks {
					_, _, _, _, err := amp.Reassemble(chunk, time.Now())
					assert.NoError(t, err, "Expected Reassemble returns no error")
				}
				count, _, err := amp.ExpireByAppID(tt.appIDArg)
				assert.Equal(t, tt.expectedCount, count, "Expected ExpireByAppID returns correct count")
				assert.NoError(t, err, "Expected ExpireByAppID return error")
			})
		}
	})
}

func TestMakeHolderKey(t *testing.T) {
	var tests = []struct {
		name string
		id1  hb.ApplicationMessageID
		id2  hb.ApplicationMessageID
	}{
		{
			name: "WithPossibleClash1",
			id1: hb.ApplicationMessageID{
				ValidStart: &timestamp.Timestamp{
					Seconds: 1,
					Nanos:   0,
				},
				AccountID: &hb.AccountID{
					ShardNum:   0,
					RealmNum:   0,
					AccountNum: 18650,
				},
				Metadata: &any.Any{
					Value: []byte("sample metadata1"),
				},
			},
			id2: hb.ApplicationMessageID{
				ValidStart: &timestamp.Timestamp{
					Seconds: 11,
					Nanos:   0,
				},
				AccountID: &hb.AccountID{
					ShardNum:   0,
					RealmNum:   0,
					AccountNum: 18650,
				},
				Metadata: &any.Any{
					Value: []byte("sample metadata"),
				},
			},
		},
		{
			name: "WithPossibleClash2",
			id1: hb.ApplicationMessageID{
				ValidStart: &timestamp.Timestamp{
					Seconds: 11,
					Nanos:   0,
				},
				AccountID: &hb.AccountID{
					ShardNum:   0,
					RealmNum:   0,
					AccountNum: 18650,
				},
				Metadata: &any.Any{
					Value: []byte("sample metadata"),
				},
			},
			id2: hb.ApplicationMessageID{
				ValidStart: &timestamp.Timestamp{
					Seconds: 1,
					Nanos:   10,
				},
				AccountID: &hb.AccountID{
					ShardNum:   0,
					RealmNum:   0,
					AccountNum: 18650,
				},
				Metadata: &any.Any{
					Value: []byte("sample metadata"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key1 := makeHolderKey(tt.id1)
			key2 := makeHolderKey(tt.id2)
			assert.NotEqual(t, key1, key2, "key1 and key2 built from id1 and id2 should be different")
		})
	}
}

func TestIsNil(t *testing.T) {
	var nilImpl *struct{}
	var tests = []struct {
		name        string
		arg         interface{}
		expectedRes bool
	}{
		{
			name:        "WithNonNilValue",
			arg:         &struct{}{},
			expectedRes: false,
		},
		{
			name:        "WithNilInterface",
			arg:         nil,
			expectedRes: true,
		},
		{
			name:        "WithNilImpl",
			arg:         nilImpl,
			expectedRes: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedRes, isNil(tt.arg))
		})
	}
}
