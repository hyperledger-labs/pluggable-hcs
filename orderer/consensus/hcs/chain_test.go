/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"context"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/rand"
	"reflect"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/hashgraph/hedera-sdk-go/v2"
	cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/common/channelconfig"
	"github.com/hyperledger/fabric/orderer/common/msgprocessor"
	"github.com/hyperledger/fabric/orderer/consensus"
	"github.com/hyperledger/fabric/orderer/consensus/hcs/factory"
	mockhcs "github.com/hyperledger/fabric/orderer/consensus/hcs/mock"
	hb "github.com/hyperledger/fabric/orderer/consensus/hcs/protodef"
	mockblockcutter "github.com/hyperledger/fabric/orderer/mocks/common/blockcutter"
	mockmultichannel "github.com/hyperledger/fabric/orderer/mocks/common/multichannel"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

//go:generate counterfeiter -o mock/orderer_capabilities.go --fake-name OrdererCapabilities . ordererCapabilities
type ordererCapabilities interface {
	channelconfig.OrdererCapabilities
}

//go:generate counterfeiter -o mock/channel_capabilities.go --fake-name ChannelCapabilities . channelCapabilities
type channelCapabilities interface {
	channelconfig.ChannelCapabilities
}

//go:generate counterfeiter -o mock/channel_config.go --fake-name ChannelConfig . channelConfig
type channelConfig interface {
	channelconfig.Channel
}

//go:generate counterfeiter -o mock/hcs_client.go --fake-name HcsClient . hcsClient
type hcsClient interface {
	factory.HcsClient
}

//go:generate counterfeiter -o mock/hcs_client_factory.go --fake-name HcsClientFactory . hcsClientFactory
type hcsClientFactory interface {
	factory.HcsClientFactory
}

//go:generate counterfeiter -o mock/app_msg_processor.go --fake-name AppMsgProcessor . mockAppMsgProcessor
type mockAppMsgProcessor interface {
	appMsgProcessor
}

const (
	getHcsClientFuncName = "GetHcsClient"
	goodHcsTopicIDStr    = "0.0.19610"
)

func newMockOrderer(batchTimeout time.Duration, topicID string, publicKeys []*hb.HcsConfigPublicKey) *mockhcs.OrdererConfig {
	mockCapabilities := &mockhcs.OrdererCapabilities{}
	mockCapabilities.ResubmissionReturns(false)
	mockOrderer := &mockhcs.OrdererConfig{}
	mockOrderer.CapabilitiesReturns(mockCapabilities)
	mockOrderer.BatchTimeoutReturns(batchTimeout)
	mockOrderer.ConsensusMetadataReturns(protoutil.MarshalOrPanic(&hb.HcsConfigMetadata{
		TopicId:           topicID,
		PublicKeys:        publicKeys,
		ReassembleTimeout: "30s",
	}))
	return mockOrderer
}

func newMockChannel() *mockhcs.ChannelConfig {
	mockCapabilities := &mockhcs.ChannelCapabilities{}
	mockCapabilities.ConsensusTypeMigrationReturns(false)
	mockChannel := &mockhcs.ChannelConfig{}
	mockChannel.CapabilitiesReturns(mockCapabilities)
	mockChannel.OrdererAddressesReturns([]string{"127.0.0.1:8086", "127.0.0.2:8086", "127.0.0.3:8086"})
	return mockChannel
}

var (
	shortTimeout = 1 * time.Second
	longTimeout  = 1 * time.Hour
)

func TestChain(t *testing.T) {
	oldestConsensusTimestamp := unixEpoch
	newestConsensusTimestamp := unixEpoch.Add(time.Hour * 1000)
	lastOriginalOffsetProcessed := uint64(0)
	lastResubmittedConfigOffset := uint64(0)
	lastChunkFreeSequenceProcessed := uint64(0)

	newMocks := func(t *testing.T) (mockConsenter *consenterImpl, mockSupport *mockmultichannel.ConsenterSupport) {
		mockConsenter = &consenterImpl{
			sharedHcsConfigVal: &mockLocalConfig.Hcs,
			identityVal:        make([]byte, 16),
			metrics:            newFakeMetrics(newFakeMetricsFields()),
		}

		publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
		mockSupport = &mockmultichannel.ConsenterSupport{
			BlockCutterVal:   mockblockcutter.NewReceiver(),
			Blocks:           make(chan *cb.Block),
			ChannelIDVal:     channelNameForTest(t),
			HeightVal:        uint64(3),
			SharedConfigVal:  newMockOrderer(shortTimeout, goodHcsTopicIDStr, publicKeys),
			ChannelConfigVal: newMockChannel(),
		}
		return mockConsenter, mockSupport
	}

	waitNumBlocksUntil := func(blocks chan *cb.Block, expected int, duration time.Duration) int {
		received := 0
		timer := time.After(duration)
		for {
			if received == expected {
				return received
			}

			select {
			case _, ok := <-blocks:
				if ok {
					received++
				} else {
					return received
				}
			case <-timer:
				return received
			}
		}
	}

	t.Run("New", func(t *testing.T) {
		var tests = []struct {
			name         string
			newMocksFunc func(*testing.T) (*consenterImpl, *mockmultichannel.ConsenterSupport)
			wantErr      bool
		}{
			{
				name:         "Proper",
				newMocksFunc: newMocks,
				wantErr:      false,
			},
			{
				name: "WithCorruptedConsensusMetadata",
				newMocksFunc: func(t *testing.T) (*consenterImpl, *mockmultichannel.ConsenterSupport) {
					mockConsenter, mockSupport := newMocks(t)
					publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
					mockOrderer := newMockOrderer(shortTimeout, goodHcsTopicIDStr, publicKeys)
					mockOrderer.ConsensusMetadataReturns([]byte("corrupted data"))
					mockSupport.SharedConfigVal = mockOrderer
					return mockConsenter, mockSupport
				},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockConsenter, mockSupport := tt.newMocksFunc(t)
				mockHealthChecker := &mockhcs.HealthChecker{}
				hcf := newDefaultMockHcsClientFactory()
				chain, err := newChain(
					mockConsenter,
					mockSupport,
					mockHealthChecker,
					hcf,
					oldestConsensusTimestamp,
					lastOriginalOffsetProcessed,
					lastResubmittedConfigOffset,
					oldestConsensusTimestamp,
					lastChunkFreeSequenceProcessed,
				)

				if tt.wantErr {
					assert.Nil(t, chain, "Expected newChain return nil value")
					assert.Error(t, err, "Expected newChain to return error")
					return
				}

				assert.NotNil(t, chain, "Expected newChain return non-nil value")
				assert.NoError(t, err, "Expected newChain to return without errors")
				select {
				case <-chain.Errored():
					logger.Debug("Errored() returned a closed channel as expected")
				default:
					t.Fatal("Errored() should have returned a closed channel")
				}

				select {
				case <-chain.haltChan:
					t.Fatal("haltChan should have been open")
				default:
					logger.Debug("haltChan is open as it should be")
				}

				select {
				case <-chain.startChan:
					t.Fatal("startChan should have been open")
				default:
					logger.Debug("startChan is open as it should be")
				}

				assert.Equal(t, 1, mockHealthChecker.RegisterCheckerCallCount())
				component, _ := mockHealthChecker.RegisterCheckerArgsForCall(0)
				assert.Equal(t, goodHcsTopicIDStr, component)
				assert.Equal(t, chain.lastCutBlockNumber, mockSupport.Height()-1)
				assert.Equal(t, chain.lastConsensusTimestampPersisted, oldestConsensusTimestamp)
				assert.Equal(t, chain.lastOriginalSequenceProcessed, lastOriginalOffsetProcessed)
				assert.Equal(t, chain.lastResubmittedConfigSequence, lastResubmittedConfigOffset)
				assert.Equal(t, chain.lastChunkFreeConsensusTimestamp, oldestConsensusTimestamp)
			})
		}
	})

	t.Run("Start", func(t *testing.T) {
		mockConsenter, mockSupport := newMocks(t)
		hcf := newDefaultMockHcsClientFactory()
		chain, _ := newChain(
			mockConsenter,
			mockSupport,
			&mockhcs.HealthChecker{},
			hcf,
			oldestConsensusTimestamp,
			lastOriginalOffsetProcessed,
			lastResubmittedConfigOffset,
			oldestConsensusTimestamp,
			lastChunkFreeSequenceProcessed,
		)

		origAppMsgProcessor := chain.appMsgProcessor
		fakeAppMsgProcessor := mockhcs.AppMsgProcessor{}
		fakeAppMsgProcessor.SplitCalls(func(message []byte, validStart time.Time) ([]*hb.ApplicationMessageChunk, []byte, error) {
			return origAppMsgProcessor.Split(message, validStart)
		})
		fakeAppMsgProcessor.ReassembleCalls(func(chunk *hb.ApplicationMessageChunk, timestamp time.Time) ([]byte, []byte, int, int, error) {
			return origAppMsgProcessor.Reassemble(chunk, timestamp)
		})
		fakeAppMsgProcessor.IsPendingCalls(func() bool {
			return origAppMsgProcessor.IsPending()
		})
		expireByAppIDSyncChan := make(chan struct{})
		fakeAppMsgProcessor.ExpireByAppIDCalls(func(appID []byte) (int, int, error) {
			defer func() {
				expireByAppIDSyncChan <- struct{}{}
			}()
			return origAppMsgProcessor.ExpireByAppID(appID)
		})
		chain.appMsgProcessor = &fakeAppMsgProcessor

		chain.Start()
		select {
		case <-chain.startChan:
			logger.Debug("startChan is closed as it should be")
		case <-time.After(shortTimeout):
			t.Fatal("startChan should have been closed by now")
		}
		close(getRespSyncChan(chain.topicSubscriptionHandle))

		select {
		case <-expireByAppIDSyncChan:
		case <-time.After(shortTimeout):
			t.Fatal("ExipreByAppIDSyncChan should have been called")
		}

		chain.Halt()
		returnValues := hcf.GetReturnValues()
		assert.Equalf(t, 1, len(returnValues[getHcsClientFuncName]), "Expected %s called once")

		v := reflect.ValueOf(returnValues[getHcsClientFuncName][0]).Index(0)
		hcsClient := v.Interface().(*mockhcs.HcsClient)
		assert.Equal(t, 1, hcsClient.SubscribeTopicCallCount(), "Expected SubscribeTopic called once")
		_, start, end := hcsClient.SubscribeTopicArgsForCall(0)
		assert.Equal(t, unixEpoch, *start, "Expected startTime passed to SubscribeTopic to be unixEpoch")
		assert.Nil(t, end, "Expected endTime passed to SubscribeTopic to be unixEpoch")

		// orderer started message
		select {
		case <-chain.Errored():
		case <-time.After(shortTimeout):
			t.Fatal("errorChan should have been closed by now")
		}
		assert.Equal(t, 1, fakeAppMsgProcessor.SplitCallCount(), "Expected Split called one time")
		assert.Equal(t, 1, fakeAppMsgProcessor.ReassembleCallCount(), "Expected Reassemble called one time")
		assert.Equal(t, 1, fakeAppMsgProcessor.ExpireByAppIDCallCount(), "Expected ExpireByAppID called one time")

		argOfExpireByAppID := fakeAppMsgProcessor.ExpireByAppIDArgsForCall(0)
		assert.Equal(t, chain.appID, argOfExpireByAppID, "Expected ExpireBYAppID called with self appID")
	})

	t.Run("StartWithNonUnixEpochLastConsensusTimestamp", func(t *testing.T) {
		mockConsenter, mockSupport := newMocks(t)
		hcf := newDefaultMockHcsClientFactory()
		chain, _ := newChain(
			mockConsenter,
			mockSupport,
			&mockhcs.HealthChecker{},
			hcf,
			newestConsensusTimestamp,
			lastOriginalOffsetProcessed,
			lastResubmittedConfigOffset,
			newestConsensusTimestamp,
			lastChunkFreeSequenceProcessed,
		)

		chain.Start()
		select {
		case <-chain.startChan:
			logger.Debug("startChan is closed as it should be")
		case <-time.After(shortTimeout):
			t.Fatal("startChan should have been closed by now")
		}

		// Trigger the haltChan clause in the processMessagesToBlocks goroutine
		close(chain.haltChan)
		returnValues := hcf.GetReturnValues()
		assert.Equalf(t, 1, len(returnValues[getHcsClientFuncName]), "Expected %s called once", getHcsClientFuncName)

		v := reflect.ValueOf(returnValues[getHcsClientFuncName][0]).Index(0)
		hcsClient := v.Interface().(*mockhcs.HcsClient)
		assert.Equal(t, 1, hcsClient.SubscribeTopicCallCount(), "Expected SubscribeTopic called once")
		_, start, end := hcsClient.SubscribeTopicArgsForCall(0)
		assert.Equal(t, newestConsensusTimestamp.Add(time.Nanosecond), *start, "Expected startTime passed to SubscribeTopic to be unixEpoch")
		assert.Nil(t, end, "Expected endTime passed to SubscribeTopic to be unixEpoch")
	})

	t.Run("StartWithUnexpectedSequenceNumber", func(t *testing.T) {
		mockConsenter, mockSupport := newMocks(t)
		hcf := newDefaultMockHcsClientFactory()
		chain, _ := newChain(
			mockConsenter,
			mockSupport,
			&mockhcs.HealthChecker{},
			hcf,
			newestConsensusTimestamp,
			lastOriginalOffsetProcessed,
			lastResubmittedConfigOffset,
			newestConsensusTimestamp,
			lastChunkFreeSequenceProcessed,
		)
		hcf.withInitTopicState(*chain.topicID, topicState{
			consensusTimestamp: newestConsensusTimestamp.Add(time.Nanosecond),
			sequenceNumber:     lastChunkFreeSequenceProcessed + 2,
		})

		assert.Panics(t, func() { startThread(chain) }, "Expect startThread to panic with unexpected response.sequenceNumber")
	})

	t.Run("WaitReady", func(t *testing.T) {
		t.Run("NotStarted", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			mockHealthChecker := &mockhcs.HealthChecker{}
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(
				mockConsenter,
				mockSupport,
				mockHealthChecker,
				hcf,
				oldestConsensusTimestamp,
				lastOriginalOffsetProcessed,
				lastResubmittedConfigOffset,
				oldestConsensusTimestamp,
				lastChunkFreeSequenceProcessed,
			)

			assert.Errorf(t, chain.WaitReady(), "Expected WaitReady returns error")
		})

		t.Run("ProperAfterStarted", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			mockHealthChecker := &mockhcs.HealthChecker{}
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(
				mockConsenter,
				mockSupport,
				mockHealthChecker,
				hcf,
				oldestConsensusTimestamp,
				lastOriginalOffsetProcessed,
				lastResubmittedConfigOffset,
				oldestConsensusTimestamp,
				lastChunkFreeSequenceProcessed,
			)

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}
			close(getRespSyncChan(chain.topicSubscriptionHandle))

			assert.NoError(t, chain.WaitReady(), "Expected WaitReady returns no error")
		})

		t.Run("AfterHalted", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			mockHealthChecker := &mockhcs.HealthChecker{}
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(
				mockConsenter,
				mockSupport,
				mockHealthChecker,
				hcf,
				oldestConsensusTimestamp,
				lastOriginalOffsetProcessed,
				lastResubmittedConfigOffset,
				oldestConsensusTimestamp,
				lastChunkFreeSequenceProcessed,
			)

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}
			close(getRespSyncChan(chain.topicSubscriptionHandle))

			chain.Halt()
			assert.Error(t, chain.WaitReady(), "Expected WaitReady returns error")
		})
	})

	t.Run("RecollectPendingChunks", func(t *testing.T) {
		t.Run("StartWithInvalidLastChunkFreeConsensusTimestamp", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(
				mockConsenter,
				mockSupport,
				&mockhcs.HealthChecker{},
				hcf,
				oldestConsensusTimestamp,
				lastOriginalOffsetProcessed,
				lastResubmittedConfigOffset,
				oldestConsensusTimestamp.Add(time.Nanosecond),
				lastChunkFreeSequenceProcessed,
			)

			assert.Panics(t, func() { startThread(chain) }, "Expected panic when lastChunkFreeConsensusTimestamp > lastConsensusTimestampPersisted")
		})

		t.Run("StartProper", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			// the noop consensus client wil drop any submitted messages, so the orderer started message will not cause panic
			hcf := newMockHcsClientFactoryWithReadonlyHcsClient()
			lastChunkFreeBlockConsensusTimestamp := newestConsensusTimestamp.Add(-10 * time.Minute)
			chain, _ := newChain(
				mockConsenter,
				mockSupport,
				&mockhcs.HealthChecker{},
				hcf,
				newestConsensusTimestamp,
				lastOriginalOffsetProcessed,
				lastResubmittedConfigOffset,
				lastChunkFreeBlockConsensusTimestamp,
				lastChunkFreeSequenceProcessed,
			)

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}

			var err error
			doneWait := make(chan struct{})
			go func() {
				err = chain.WaitReady()
				close(doneWait)
			}()

			select {
			case <-doneWait:
				t.Fatal("Expected WaitReady blocked when collecting pending chunks")
			case <-time.After(shortTimeout / 2):
			}

			// halt chain
			chain.Halt()
			select {
			case <-doneWait:
			case <-time.After(shortTimeout):
				t.Fatal("Expected WaitReady returned after chain is halted")
			}

			assert.Error(t, err, "Expected WaitReady return error when chain is halted before recollecting chunks is done")

			hcsClient := chain.hcsClient.(*mockhcs.HcsClient)
			assert.Equal(t, 1, hcsClient.SubscribeTopicCallCount(), "Expected SubscribeTopicCall called one time")
			_, startTime, _ := hcsClient.SubscribeTopicArgsForCall(0)
			assert.Equal(t, time.Nanosecond, startTime.Sub(lastChunkFreeBlockConsensusTimestamp),
				"Expected SubscribeTopic called a 1ns after lastChunkFreeBlockConsensusTimestamp startTime")
		})

		// verifies when started with pending chunks to recollect, the orderer can recollect all missing chunks
		// and resume normal processing
		t.Run("ProperReprocess", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			hcf := newMockHcsClientFactoryWithReadonlyHcsClient()
			lastChunkFreeBlockConsensusTimestamp := newestConsensusTimestamp.Add(-(normalReassembleTimeout / 2))
			chain, _ := newChain(
				mockConsenter,
				mockSupport,
				&mockhcs.HealthChecker{},
				hcf,
				newestConsensusTimestamp,
				lastOriginalOffsetProcessed,
				lastResubmittedConfigOffset,
				lastChunkFreeBlockConsensusTimestamp,
				lastChunkFreeSequenceProcessed,
			)
			lastCutBlockNumber := chain.lastCutBlockNumber

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}

			var err error
			doneWait := make(chan struct{})
			go func() {
				err = chain.WaitReady()
				close(doneWait)
			}()

			defer close(mockSupport.BlockCutterVal.Block)
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)
			setNextConsensusTimestamp(chain.topicSubscriptionHandle, lastChunkFreeBlockConsensusTimestamp.Add(time.Nanosecond))
			data := make([]byte, maxConsensusMessageSize*5+10)
			hcsMessage := newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelopeWithRawData(data)), uint64(0), uint64(0))
			chunksOfMsg1, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(hcsMessage), time.Now())
			assert.True(t, len(chunksOfMsg1) > 1, "Expected more than one chunks created for message 1")
			assert.NoError(t, err, "Expected Split returns no error")

			hcsMessage = newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelope("short test message")), uint64(0), uint64(0))
			chunksOfMsg2, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(hcsMessage), time.Now())
			assert.Equal(t, 1, len(chunksOfMsg2), "Expected one chunk created for message 2")
			assert.NoError(t, err, "Expected Split returns no error")

			// send all chunks of message 1 but last
			index := 0
			for ; index < len(chunksOfMsg1)-1; index++ {
				hcf.InjectMessage(protoutil.MarshalOrPanic(chunksOfMsg1[index]), chain.topicID)
				respSyncChan <- struct{}{}
			}

			select {
			case <-doneWait:
				t.Fatal("Expected WaitReady blocked when collecting pending chunks")
			case <-time.After(shortTimeout / 2):
			}

			// send the only chunk of message 2 with consensus timestamp = chain.lastConsensusTimestampPersisted
			setNextConsensusTimestamp(chain.topicSubscriptionHandle, chain.lastConsensusTimestampPersisted)
			hcf.InjectMessage(protoutil.MarshalOrPanic(chunksOfMsg2[0]), chain.topicID)
			respSyncChan <- struct{}{}

			select {
			case <-doneWait:
				assert.NoError(t, err, "Expected WaitReady is unblocked and returns no errors")
			case <-time.After(shortTimeout / 2):
				t.Fatal("Expected WaitReady no longer blocks")
			}
			assert.True(t, chain.appMsgProcessor.IsPending(), "Expected there are messages pending reassembly")

			mockSupport.BlockCutterVal.CutNext = true
			// send the last chunk of message 1
			hcf.InjectMessage(protoutil.MarshalOrPanic(chunksOfMsg1[index]), chain.topicID)
			respSyncChan <- struct{}{}
			mockSupport.BlockCutterVal.Block <- struct{}{}

			assert.Equal(t, 1, waitNumBlocksUntil(mockSupport.Blocks, 1, shortTimeout), "Expected one block cut")

			chain.Halt()
			assert.Equal(t, lastCutBlockNumber+1, chain.lastCutBlockNumber, "Expected lastCutBlockNumber increased by 1")
			assert.False(t, chain.appMsgProcessor.IsPending(), "Expected no more pending chunks")
		})

		t.Run("ProperBlockMetadataWhenHaltWithPendingChunks", func(t *testing.T) {
			// start with no pending chunks to recollect
			mockConsenter, mockSupport := newMocks(t)
			hcf := newMockHcsClientFactoryWithReadonlyHcsClient()
			chain, _ := newChain(
				mockConsenter,
				mockSupport,
				&mockhcs.HealthChecker{},
				hcf,
				oldestConsensusTimestamp,
				lastOriginalOffsetProcessed,
				lastResubmittedConfigOffset,
				oldestConsensusTimestamp,
				lastChunkFreeSequenceProcessed,
			)
			lastCutBlockNumber := chain.lastCutBlockNumber

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}

			close(mockSupport.BlockCutterVal.Block)
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)
			mockSupport.BlockCutterVal.CutNext = true

			data := make([]byte, maxConsensusMessageSize*2+10)
			hcsMessage := newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelopeWithRawData(data)), uint64(0), uint64(0))
			chunksOfMsg1, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(hcsMessage), time.Now())
			assert.True(t, len(chunksOfMsg1) > 1, "Expected more than one chunks created for message 1")
			assert.NoError(t, err, "Expected Split returns no error")

			data = make([]byte, maxConsensusMessageSize*3+10)
			hcsMessage = newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelopeWithRawData(data)), uint64(0), uint64(0))
			chunksOfMsg2, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(hcsMessage), time.Now())
			assert.True(t, len(chunksOfMsg2) > 1, "Expected more than one chunks created for message 2")
			assert.NoError(t, err, "Expected Split returns no error")

			data = make([]byte, maxConsensusMessageSize*2+10)
			hcsMessage = newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelopeWithRawData(data)), uint64(0), uint64(0))
			chunksOfMsg3, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(hcsMessage), time.Now())
			assert.True(t, len(chunksOfMsg3) > 1, "Expected more than one chunks created for message 3")
			assert.NoError(t, err, "Expected Split returns no error")

			// send all chunks of message 1, one block should be cut
			setNextConsensusTimestamp(chain.topicSubscriptionHandle, oldestConsensusTimestamp.Add(time.Nanosecond))
			var expectedBlock1ConsensusTimestamp time.Time
			var expectedBlock1Sequence uint64
			for index, chunk := range chunksOfMsg1 {
				if index == len(chunksOfMsg1)-1 {
					expectedBlock1ConsensusTimestamp = getNextConsensusTimestamp(chain.topicSubscriptionHandle)
					expectedBlock1Sequence = getNextSequenceNumber(chain.topicSubscriptionHandle)
				}
				hcf.InjectMessage(protoutil.MarshalOrPanic(chunk), chain.topicID)
				respSyncChan <- struct{}{}
			}
			var block1 *cb.Block
			select {
			case block1 = <-mockSupport.Blocks:
			case <-time.After(shortTimeout):
				t.Fatal("Expected one block cut")
			}

			// send all chunks of message 2 but last
			for index := 0; index < len(chunksOfMsg2)-1; index++ {
				hcf.InjectMessage(protoutil.MarshalOrPanic(chunksOfMsg2[index]), chain.topicID)
				respSyncChan <- struct{}{}
			}

			// send all chunks of message 3, a second block should be cut
			var expectedBlock2ConsensusTimestamp time.Time
			for index, chunk := range chunksOfMsg3 {
				if index == len(chunksOfMsg3)-1 {
					expectedBlock2ConsensusTimestamp = getNextConsensusTimestamp(chain.topicSubscriptionHandle)
				}
				hcf.InjectMessage(protoutil.MarshalOrPanic(chunk), chain.topicID)
				respSyncChan <- struct{}{}
			}
			var block2 *cb.Block
			select {
			case block2 = <-mockSupport.Blocks:
			case <-time.After(shortTimeout):
				t.Fatal("Expected one block cut")
			}

			assert.True(t, chain.appMsgProcessor.IsPending(), "Expected there are messages pending reassembly")

			chain.Halt()
			assert.Equal(t, lastCutBlockNumber+2, chain.lastCutBlockNumber, "Expected lastCutBlockNumber increased by 2")
			assert.Equal(t, timestampProtoOrPanic(expectedBlock1ConsensusTimestamp), extractConsensusTimestamp(block1), "Expected consensus timestamp of block one to match")
			assert.Equal(t, timestampProtoOrPanic(expectedBlock2ConsensusTimestamp), extractConsensusTimestamp(block2), "Expected consensus timestamp of block two to match")
			assert.Equal(t, timestampProtoOrPanic(expectedBlock1ConsensusTimestamp), extractLastChunkFreeConsensusTimestamp(block1), "Expected correct lastChunkFreeConsensusTimestamp in block1")
			assert.Equal(t, timestampProtoOrPanic(expectedBlock1ConsensusTimestamp), extractLastChunkFreeConsensusTimestamp(block2), "Expected correct lastChunkFreeConsensusTimestamp in block2")
			assert.Equal(t, expectedBlock1Sequence, extractLastChunkFreeSequence(block1), "Expected correct lastChunkFreeSequence in block1")
			assert.Equal(t, expectedBlock1Sequence, extractLastChunkFreeSequence(block2), "Expected correct lastChunkFreeSequence in block2")
		})
	})

	t.Run("Halt", func(t *testing.T) {
		mockConsenter, mockSupport := newMocks(t)
		hcf := newDefaultMockHcsClientFactory()
		chain, _ := newChain(
			mockConsenter,
			mockSupport,
			&mockhcs.HealthChecker{},
			hcf,
			oldestConsensusTimestamp,
			lastOriginalOffsetProcessed,
			lastResubmittedConfigOffset,
			oldestConsensusTimestamp,
			lastChunkFreeSequenceProcessed,
		)

		chain.Start()
		select {
		case <-chain.startChan:
			logger.Debug("startChan is closed as it should be")
		case <-time.After(shortTimeout):
			t.Fatal("startChan should have been closed by now")
		}
		close(getRespSyncChan(chain.topicSubscriptionHandle))

		closeCallChan := make(chan string)
		mockHcsClient := chain.hcsClient.(*mockhcs.HcsClient)
		mockHcsClient.CloseCalls(func() error {
			closeCallChan <- "HcsClient"
			return nil
		})

		done := make(chan struct{})
		closeCallInfo := map[string]int{}
		go func() {
			timer := time.After(shortTimeout)
			for {
				var expired bool
				select {
				case cname := <-closeCallChan:
					closeCallInfo[cname] = closeCallInfo[cname] + 1
				case <-timer:
					expired = true
				}

				if expired {
					break
				}
			}
			done <- struct{}{}
		}()

		// Wait till the start phase has completed, then:
		chain.Halt()

		select {
		case <-chain.haltChan:
			logger.Debug("haltChan is closed as it should be")
		case <-time.After(shortTimeout):
			t.Fatal("haltChan should have been closed")
		}

		select {
		case <-chain.errorChan:
			logger.Debug("errorChan is closed as it should be")
		case <-time.After(shortTimeout):
			t.Fatal("errorChan should have been closed")
		}

		<-done
		assert.Equal(t, 1, closeCallInfo["HcsClient"], "Expected Closed() called once on HcsClient")
	})

	t.Run("DoubleHalt", func(t *testing.T) {
		mockConsenter, mockSupport := newMocks(t)
		hcf := newDefaultMockHcsClientFactory()
		chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)

		chain.Start()
		select {
		case <-chain.startChan:
			logger.Debug("startChan is closed as it should be")
		case <-time.After(shortTimeout):
			t.Fatal("startChan should have been closed by now")
		}

		chain.Halt()
		assert.NotPanics(t, func() { chain.Halt() }, "Calling Halt() more than once shouldn't panic")
	})

	t.Run("HaltBeforeStart", func(t *testing.T) {
		mockConsenter, mockSupport := newMocks(t)
		hcf := newDefaultMockHcsClientFactory()
		chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)

		go func() {
			time.Sleep(shortTimeout)
			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}
		}()

		done := make(chan struct{})
		go func() {
			chain.Halt()
			close(done)
		}()
		// halt should return once chain is started
		select {
		case <-done:
			logger.Debug("Halt returns as expected")
		case <-time.After(3 * shortTimeout):
			close(chain.startChan)
			t.Fatalf("Halt should have returned")
		}
	})

	t.Run("StartWithHcsClientError", func(t *testing.T) {
		mockConsenter, mockSupport := newMocks(t)
		getHcsClient := func(map[string]hedera.AccountID, string, *hedera.AccountID, *hedera.PrivateKey) (factory.HcsClient, error) {
			return nil, fmt.Errorf("foo error")
		}
		hcf := newMockHcsClientFactory(getHcsClient, false)
		chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)

		assert.Panics(t, func() { startThread(chain) }, "Expected the Start() call to panic")
	})

	t.Run("StartWithTopicSubscriptionError", func(t *testing.T) {
		mockConsenter, mockSupport := newMocks(t)
		getHcsClient := func(map[string]hedera.AccountID, string, *hedera.AccountID, *hedera.PrivateKey) (factory.HcsClient, error) {
			c := mockhcs.HcsClient{}
			c.SubscribeTopicCalls(func(*hedera.TopicID, *time.Time, *time.Time) (factory.MirrorSubscriptionHandle, error) {
				return nil, fmt.Errorf("foo error")
			})
			return &c, nil
		}
		hcf := newMockHcsClientFactory(getHcsClient, false)
		chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)

		assert.Panics(t, func() { startThread(chain) }, "Expected the Start() call to panic")
	})

	t.Run("enqueue", func(t *testing.T) {
		t.Run("Proper", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			hcf := newMockHcsClientFactoryWithReadonlyHcsClient()
			chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}

			ok, _ := chain.enqueue(newNormalMessage([]byte("testMessage"), uint64(0), uint64(0)), false)
			assert.True(t, ok, "Expect enqueue call to return true")
			chain.Halt()
		})

		t.Run("WithChainNotStarted", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)

			// We don't need to create a legit envelope here as it's not inspected during this test
			ok, _ := chain.enqueue(newNormalMessage([]byte("testMessage"), uint64(1), uint64(0)), false)
			assert.False(t, ok, "Expected enqueue call to return false")
		})

		t.Run("WithChainHalted", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}
			chain.Halt()

			// haltChan should close access to the post path.
			// We don't need to create a legit envelope here as it's not inspected during this test
			ok, _ := chain.enqueue(newNormalMessage([]byte("testMessage"), uint64(0), uint64(0)), false)
			assert.False(t, ok, "Expected enqueue call to return false")
		})

		grpcCodeInternal := codes.Internal
		t.Run("WithError", func(t *testing.T) {
			var tests = []struct {
				name                  string
				err                   error
				errCount              int
				expectFailure         bool
				expectSubmitCallCount int
			}{
				{
					name:          "UnrecoverableError",
					err:           fmt.Errorf("failed to send the message"),
					errCount:      1,
					expectFailure: true,
				},
				{
					name: "ProperWithPreCheckDuplicationTransaction",
					err: hedera.ErrHederaPreCheckStatus{
						TxID:   hedera.TransactionID{},
						Status: hedera.StatusDuplicateTransaction,
					},
					errCount:              1,
					expectFailure:         false,
					expectSubmitCallCount: 2,
				},
				{
					name: "ProperWithPreCheckDuplicationTransaction",
					err: hedera.ErrHederaPreCheckStatus{
						TxID:   hedera.TransactionID{},
						Status: hedera.StatusBusy,
					},
					errCount:              1,
					expectFailure:         false,
					expectSubmitCallCount: 2,
				},
				{
					name: "ProperWithGrpcCodeInternal",
					err: hedera.ErrHederaNetwork{
						StatusCode: &grpcCodeInternal,
					},
					errCount:              1,
					expectFailure:         false,
					expectSubmitCallCount: 2,
				},
				{
					name: "FailWithPreCheckDuplicationTransactionAndRetryExceeded",
					err: hedera.ErrHederaPreCheckStatus{
						TxID:   hedera.TransactionID{},
						Status: hedera.StatusBusy,
					},
					errCount:      10,
					expectFailure: true,
				},
				{
					name: "FailWithPreCheckStatusBusyAndRetryExceeded",
					err: hedera.ErrHederaPreCheckStatus{
						TxID:   hedera.TransactionID{},
						Status: hedera.StatusBusy,
					},
					errCount:      10,
					expectFailure: true,
				},
				{
					name: "FailWithGrpcCodeInternalAndRetryExceeded",
					err: hedera.ErrHederaNetwork{
						StatusCode: &grpcCodeInternal,
					},
					errCount:      10,
					expectFailure: true,
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					mockConsenter, mockSupport := newMocks(t)
					// the noop consensus client will silently drop the orderer started message sent during chain bootstrapping
					hcf := newMockHcsClientFactoryWithReadonlyHcsClient()
					chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)

					chain.Start()
					select {
					case <-chain.startChan:
						logger.Debug("startChan is closed as it should be")
					case <-time.After(shortTimeout):
						t.Fatal("startChan should have been closed by now")
					}
					defer chain.Halt()
					c := chain.hcsClient.(*mockhcs.HcsClient)
					count := 0
					c.SubmitConsensusMessageCalls(func(_ []byte, _ *hedera.TopicID, txID *hedera.TransactionID) (*hedera.TransactionID, error) {
						defer func() {
							count++
						}()
						if count < tt.errCount {
							return nil, tt.err
						}
						if txID == nil {
							txID = &hedera.TransactionID{}
						}
						return txID, nil
					})
					callCount := c.SubmitConsensusMessageCallCount()

					ret, _ := chain.enqueue(newNormalMessage([]byte("testMessage"), uint64(0), uint64(0)), false)
					if tt.expectFailure {
						assert.False(t, ret, "Expected enqueue to return false")
					} else {
						assert.True(t, ret, "Expected enqueue to return true")
						assert.Equal(t, tt.expectSubmitCallCount, c.SubmitConsensusMessageCallCount()-callCount)
					}
				})
			}
		})
	})

	t.Run("Order", func(t *testing.T) {
		t.Run("ErrorIfNotStarted", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)

			// We don't need to create a legit envelope here as it's not inspected during this test
			assert.Error(t, chain.Order(&cb.Envelope{}, uint64(0)))
		})

		t.Run("Proper", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			mockSupport.BlockCutterVal = mockblockcutter.NewReceiver()
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)
			lastCutBlockNumber := chain.lastCutBlockNumber

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}
			close(getRespSyncChan(chain.topicSubscriptionHandle))

			assert.NoError(t, chain.Order(&cb.Envelope{}, uint64(0)), "Expect Order successfully")
			close(mockSupport.BlockCutterVal.Block)
			chain.Halt()
			assert.Equal(t, lastCutBlockNumber, chain.lastCutBlockNumber, "Expect no block cut")
		})

		t.Run("TwoSingleEnvBlocks", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			mockSupport.Blocks = make(chan *cb.Block)
			mockSupport.BlockCutterVal = mockblockcutter.NewReceiver()
			mockSupport.BlockCutterVal.CutNext = true
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)
			savedLastCubBlockNumber := chain.lastCutBlockNumber
			close(mockSupport.BlockCutterVal.Block)

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}
			close(getRespSyncChan(chain.topicSubscriptionHandle))

			assert.NoError(t, chain.Order(&cb.Envelope{}, uint64(0)), "Expect Order successfully")
			select {
			case <-mockSupport.Blocks:
			case <-time.After(shortTimeout):
				t.Fatalf("Expected one block written")
			}
			assert.NoError(t, chain.Order(&cb.Envelope{}, uint64(0)), "Expect Order successfully")
			select {
			case <-mockSupport.Blocks:
			case <-time.After(shortTimeout):
				t.Fatalf("Expected one block written")
			}

			chain.Halt()
			assert.Equal(t, savedLastCubBlockNumber+2, chain.lastCutBlockNumber, "Expected two blocks cut")
		})

		t.Run("BatchLengthTwo", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			mockSupport.Blocks = make(chan *cb.Block)
			mockSupport.BlockCutterVal = mockblockcutter.NewReceiver()
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)
			savedLastCubBlockNumber := chain.lastCutBlockNumber

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}
			close(getRespSyncChan(chain.topicSubscriptionHandle))

			assert.NoError(t, chain.Order(&cb.Envelope{}, uint64(0)), "Expect Order successfully")
			mockSupport.BlockCutterVal.Block <- struct{}{}
			close(mockSupport.BlockCutterVal.Block)
			mockSupport.BlockCutterVal.IsolatedTx = true
			assert.NoError(t, chain.Order(&cb.Envelope{}, uint64(0)), "Expect Order successfully")
			numBlocksWritten := waitNumBlocksUntil(mockSupport.Blocks, 2, shortTimeout)
			chain.Halt()
			assert.Equal(t, savedLastCubBlockNumber+2, chain.lastCutBlockNumber, "Expect two blocks cut")
			assert.Equal(t, 2, numBlocksWritten, "Expect two blocks written")
		})
	})

	t.Run("Configure", func(t *testing.T) {
		t.Run("ErrorIfNotStarted", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)

			// We don't need to create a legit envelope here as it's not inspected during this test
			assert.Error(t, chain.Configure(&cb.Envelope{}, uint64(0)))
		})

		t.Run("Proper", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			mockSupport.Blocks = make(chan *cb.Block)
			mockSupport.BlockCutterVal = mockblockcutter.NewReceiver()
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)
			savedLastCutBlockNumber := chain.lastCutBlockNumber
			// no synchronization with blockcutter needed
			close(mockSupport.BlockCutterVal.Block)

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}
			close(getRespSyncChan(chain.topicSubscriptionHandle))

			// We don't need to create a legit envelope here as it's not inspected during this test
			assert.NoError(t, chain.Configure(&cb.Envelope{}, uint64(0)), "Expect Configure successfully")
			numBlocksWritten := waitNumBlocksUntil(mockSupport.Blocks, 1, shortTimeout)
			chain.Halt()
			assert.Equal(t, savedLastCutBlockNumber+1, chain.lastCutBlockNumber, "Expect one block cut")
			assert.Equal(t, 1, numBlocksWritten, "Expect one block written")
		})

		t.Run("ProperWithPendingNormalMessage", func(*testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			mockSupport.Blocks = make(chan *cb.Block)
			mockSupport.BlockCutterVal = mockblockcutter.NewReceiver()
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)
			savedLastCutBlockNumber := chain.lastCutBlockNumber
			// no synchronization with blockcutter needed
			close(mockSupport.BlockCutterVal.Block)

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatalf("startChan should have been closed by now")
			}
			close(getRespSyncChan(chain.topicSubscriptionHandle))

			assert.NoError(t, chain.Order(&cb.Envelope{}, uint64(0)), "Expect Order successfully")
			assert.NoError(t, chain.Configure(&cb.Envelope{}, uint64(0)), "Expect Configure successfully")
			numBlocksWritten := waitNumBlocksUntil(mockSupport.Blocks, 2, shortTimeout)
			chain.Halt()
			assert.Equal(t, savedLastCutBlockNumber+2, chain.lastCutBlockNumber, "Expect two blocks cut")
			assert.Equal(t, 2, numBlocksWritten, "Expect two blocks written")
		})
	})

	t.Run("TimeToCut", func(t *testing.T) {
		t.Run("Proper", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			mockSupport.Blocks = make(chan *cb.Block)
			mockSupport.BlockCutterVal = mockblockcutter.NewReceiver()
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)
			savedLastCubBlockNumber := chain.lastCutBlockNumber
			close(mockSupport.BlockCutterVal.Block)

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}
			close(getRespSyncChan(chain.topicSubscriptionHandle))

			assert.NoError(t, chain.Order(&cb.Envelope{}, uint64(0)), "Expect Order successfully")
			numBlocksWritten := waitNumBlocksUntil(mockSupport.Blocks, 1, 3*shortTimeout)
			chain.Halt()
			assert.Equal(t, savedLastCubBlockNumber+1, chain.lastCutBlockNumber, "Expect one block cut")
			assert.Equal(t, 1, numBlocksWritten, "Expect one block written")
		})

		t.Run("WithError", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			mockGetHcsClient := func(
				map[string]hedera.AccountID,
				string,
				*hedera.AccountID,
				*hedera.PrivateKey,
			) (factory.HcsClient, error) {
				c := mockhcs.HcsClient{}
				c.SubmitConsensusMessageReturns(nil, fmt.Errorf("foo error"))
				return &c, nil
			}
			hcf := newMockHcsClientFactory(mockGetHcsClient, false)
			chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)

			assert.Error(t, chain.sendTimeToCut(newTimeToCutMessageWithBlockNumber(5)), "Expect error from sendTimeToCut")
		})
	})

	t.Run("SubscriptionStreamingError", func(t *testing.T) {
		recoverableCodes := []codes.Code{codes.NotFound, codes.ResourceExhausted, codes.Unavailable}

		t.Run("UnrecoverableStatus", func(t *testing.T) {
			mockConsenter, mockSupport := newMocks(t)
			mockSupport.Blocks = make(chan *cb.Block)
			mockSupport.BlockCutterVal = mockblockcutter.NewReceiver()
			hcf := newDefaultMockHcsClientFactory()
			chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)
			close(mockSupport.BlockCutterVal.Block)

			chain.Start()
			select {
			case <-chain.startChan:
				logger.Debug("startChan is closed as it should be")
			case <-time.After(shortTimeout):
				t.Fatal("startChan should have been closed by now")
			}

			done := make(chan struct{})
			go func() {
				<-chain.Errored()
				done <- struct{}{}
			}()
			close(getRespSyncChan(chain.topicSubscriptionHandle))
			sendError(chain.topicSubscriptionHandle, status.New(codes.Aborted, "test error message"))

			select {
			case <-done:
			case <-time.After(shortTimeout):
				t.Fatal("Expected errChan closed")
			}

			select {
			case <-chain.haltChan:
			case <-time.After(shortTimeout):
				t.Fatal("Expected haltChan closed")
			}
		})

		for _, code := range recoverableCodes {
			t.Run("ProperRetry"+code.String(), func(t *testing.T) {
				mockConsenter, mockSupport := newMocks(t)
				mockSupport.Blocks = make(chan *cb.Block)
				mockSupport.BlockCutterVal = mockblockcutter.NewReceiver()
				hcf := newDefaultMockHcsClientFactory()
				chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)
				close(mockSupport.BlockCutterVal.Block)

				chain.Start()
				select {
				case <-chain.startChan:
					logger.Debug("startChan is closed as it should be")
				case <-time.After(shortTimeout):
					t.Fatal("startChan should have been closed by now")
				}
				close(getRespSyncChan(chain.topicSubscriptionHandle))

				mockHcsClient := chain.hcsClient.(*mockhcs.HcsClient)
				oldSubscribeTopicStub := mockHcsClient.SubscribeTopicStub
				subscribeTopicSyncChan := make(chan struct{})
				topicSubscriptionHandle := chain.topicSubscriptionHandle
				mockHcsClient.SubscribeTopicCalls(func(topicID *hedera.TopicID, start *time.Time, end *time.Time) (handle factory.MirrorSubscriptionHandle, err error) {
					defer func() {
						topicSubscriptionHandle = handle
						<-subscribeTopicSyncChan
					}()
					handle, err = oldSubscribeTopicStub(topicID, start, end)
					return handle, err
				})

				// send an error to the subscription handle
				sendError(topicSubscriptionHandle, status.New(code, "Topic does not exist"))

				// let the subscription retry succeed
				subscribeTopicSyncChan <- struct{}{}

				select {
				case <-chain.Errored():
				case <-time.After(shortTimeout):
					t.Fatal("Expected errorChan is closed")
				}

				// based on the fact the mock implementation always advances timestamp by 1ns
				expectedSubscriptionStartTimestamp := getNextConsensusTimestamp(topicSubscriptionHandle)
				close(getRespSyncChan(topicSubscriptionHandle))

				blocked := false
				for i := 0; i < 5; i++ {
					time.Sleep(shortTimeout / 10)
					select {
					case <-chain.Errored():
					default:
						blocked = true
						break
					}
				}
				assert.True(t, blocked, "Expected errorChan blocks")
				assert.Equal(t, 2, mockHcsClient.SubscribeTopicCallCount(), "Expected SubscribeTopic called twice")
				_, lastSubscriptionStartTimestamp, _ := mockHcsClient.SubscribeTopicArgsForCall(1)
				assert.Equal(t, expectedSubscriptionStartTimestamp, *lastSubscriptionStartTimestamp, "Expected correct subscription start timestamp")
				chain.Halt()
			})
		}

		for _, code := range recoverableCodes {
			t.Run("RetryMaxExceeded"+code.String(), func(t *testing.T) {
				if testing.Short() {
					t.Skip("Skipping test in short mode")
				}

				mockConsenter, mockSupport := newMocks(t)
				mockSupport.Blocks = make(chan *cb.Block)
				mockSupport.BlockCutterVal = mockblockcutter.NewReceiver()
				hcf := newDefaultMockHcsClientFactory()
				chain, _ := newChain(mockConsenter, mockSupport, &mockhcs.HealthChecker{}, hcf, oldestConsensusTimestamp, lastOriginalOffsetProcessed, lastResubmittedConfigOffset, oldestConsensusTimestamp, lastChunkFreeSequenceProcessed)
				close(mockSupport.BlockCutterVal.Block)

				chain.Start()
				select {
				case <-chain.startChan:
					logger.Debug("startChan is closed as it should be")
				case <-time.After(shortTimeout):
					t.Fatal("startChan should have been closed by now")
				}
				close(getRespSyncChan(chain.topicSubscriptionHandle))
				mockHcsClient := chain.hcsClient.(*mockhcs.HcsClient)
				mockHcsClient.SubmitConsensusMessageCalls(func(_ []byte, _ *hedera.TopicID, txID *hedera.TransactionID) (*hedera.TransactionID, error) {
					if txID == nil {
						txID = &hedera.TransactionID{}
					}
					return txID, nil
				})
				oldSubscribeTopicStub := mockHcsClient.SubscribeTopicStub
				subscribeTopicSyncChan := make(chan factory.MirrorSubscriptionHandle)
				mockHcsClient.SubscribeTopicCalls(func(topicID *hedera.TopicID, start *time.Time, end *time.Time) (handle factory.MirrorSubscriptionHandle, err error) {
					defer func() {
						subscribeTopicSyncChan <- handle
					}()
					handle, err = oldSubscribeTopicStub(topicID, start, end)
					return handle, err
				})

				handle := chain.topicSubscriptionHandle
				for i := 0; i < subscriptionRetryMax; i++ {
					sendError(handle, status.New(code, "Topic does not exist"))
					handle = <-subscribeTopicSyncChan
				}
				// this last error should cause retry count exceed the max, thus errChan will be closed
				sendError(handle, status.New(code, "Topic does not exist"))

				select {
				case <-chain.Errored():
				case <-time.After(shortTimeout):
					t.Fatalf("Expected errChan closed")
				}

				select {
				case <-chain.haltChan:
				case <-time.After(shortTimeout):
					t.Fatal("Expected haltChan closed")
				}
				assert.Equal(t, 1+subscriptionRetryMax, mockHcsClient.SubscribeTopicCallCount(), "Expected SubscribeTopic called 1 + subscriptionRetryMax times")
			})
		}
	})
}

func TestSigner(t *testing.T) {
	newBareMinimumChainForSigner := func(privateKeyStr string, publicKeyStrs []string) *chainImpl {
		privateKey, err := hedera.PrivateKeyFromString(privateKeyStr)
		assert.NoError(t, err, "Expected valid ed25519 private key string")

		publicKeys := map[string]*hedera.PublicKey{}
		if publicKeyStrs != nil {
			for _, keyStr := range publicKeyStrs {
				publicKey, err := hedera.PublicKeyFromString(keyStr)
				assert.NoError(t, err, "Expected valid ed25519 public key string")
				publicKeys[string(publicKey.Bytes())] = &publicKey
			}
		}
		return &chainImpl{
			ConsenterSupport: &mockmultichannel.ConsenterSupport{
				ChannelIDVal: channelNameForTest(t),
			},
			operatorPrivateKey:     &privateKey,
			operatorPublicKeyBytes: privateKey.PublicKey().Bytes(),
			publicKeys:             publicKeys,
		}
	}

	var tests = []struct {
		name              string
		privateKey        string
		publicKeys        []string
		createMessageFunc func() []byte
		dataModifyFunc    func(pubKeyIn, msgIn, sigIn []byte) (pubKeyOut, msgOut, sigOut []byte)
		wantErrOnSign     bool
		wantVerifyPass    bool
	}{
		{
			name:           "Proper",
			privateKey:     testOperatorPrivateKey,
			publicKeys:     []string{testOperatorPublicKey},
			wantErrOnSign:  false,
			wantVerifyPass: true,
		},
		{
			name:              "WithNilMessage",
			privateKey:        testOperatorPrivateKey,
			publicKeys:        []string{testOperatorPublicKey},
			createMessageFunc: func() []byte { return nil },
			wantErrOnSign:     true,
		},
		{
			name:              "WithEmptyMessage",
			privateKey:        testOperatorPrivateKey,
			publicKeys:        []string{testOperatorPublicKey},
			createMessageFunc: func() []byte { return []byte{} },
			wantErrOnSign:     true,
		},
		{
			name:       "WithCorruptedMessage",
			privateKey: testOperatorPrivateKey,
			publicKeys: []string{testOperatorPublicKey},
			dataModifyFunc: func(pubKeyIn, msgIn, sigIn []byte) (pubKeyOut, msgOut, sigOut []byte) {
				msgIn[0] = ^msgIn[0]
				return pubKeyIn, msgIn, sigIn
			},
			wantErrOnSign:  false,
			wantVerifyPass: false,
		},
		{
			name:       "WithCorruptedSignature",
			privateKey: testOperatorPrivateKey,
			publicKeys: []string{testOperatorPublicKey},
			dataModifyFunc: func(pubKeyIn, msgIn, sigIn []byte) (pubKeyOut, msgOut, sigOut []byte) {
				sigIn[0] = ^sigIn[0]
				return pubKeyIn, msgIn, sigIn
			},
			wantErrOnSign:  false,
			wantVerifyPass: false,
		},
		{
			name:       "WithNoMatchingPublicKey",
			privateKey: testOperatorPrivateKey,
			publicKeys: []string{testOperatorPublicKey},
			dataModifyFunc: func(pubKeyIn, msgIn, sigIn []byte) (pubKeyOut, msgOut, sigOut []byte) {
				pubKeyIn[0] = ^pubKeyIn[0]
				return pubKeyIn, msgIn, sigIn
			},
			wantErrOnSign:  false,
			wantVerifyPass: false,
		},
		{
			name:           "WithNoPublicKey",
			privateKey:     testOperatorPrivateKey,
			wantErrOnSign:  false,
			wantVerifyPass: false,
		},
		{
			name:       "WithNilVerifyPublicKey",
			privateKey: testOperatorPrivateKey,
			publicKeys: []string{testOperatorPublicKey},
			dataModifyFunc: func(pubKeyIn, msgIn, sigIn []byte) (pubKeyOut, msgOut, sigOut []byte) {
				return nil, msgIn, sigIn
			},
			wantErrOnSign:  false,
			wantVerifyPass: false,
		},
		{
			name:       "WithNilVerifyMessage",
			privateKey: testOperatorPrivateKey,
			publicKeys: []string{testOperatorPublicKey},
			dataModifyFunc: func(pubKeyIn, msgIn, sigIn []byte) (pubKeyOut, msgOut, sigOut []byte) {
				return pubKeyIn, nil, sigIn
			},
			wantErrOnSign:  false,
			wantVerifyPass: false,
		},
		{
			name:       "WithNilVerifySignature",
			privateKey: testOperatorPrivateKey,
			publicKeys: []string{testOperatorPublicKey},
			dataModifyFunc: func(pubKeyIn, msgIn, sigIn []byte) (pubKeyOut, msgOut, sigOut []byte) {
				return pubKeyIn, msgIn, nil
			},
			wantErrOnSign:  false,
			wantVerifyPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := newBareMinimumChainForSigner(tt.privateKey, tt.publicKeys)

			var message []byte
			if tt.createMessageFunc != nil {
				message = tt.createMessageFunc()
			} else {
				message = make([]byte, 128)
				rand.Read(message)
			}
			publicKey, signature, err := chain.Sign(message)
			if tt.wantErrOnSign {
				assert.Error(t, err, "Expected Sign return error")
				return
			}
			assert.NoError(t, err, "Expected Sign return no error")

			if tt.dataModifyFunc != nil {
				publicKey, message, signature = tt.dataModifyFunc(publicKey, message, signature)
			}

			if tt.wantVerifyPass {
				assert.True(t, chain.Verify(message, publicKey, signature), "Expected Verify pass")
			} else {
				assert.False(t, chain.Verify(message, publicKey, signature), "Expected Verify fail")
			}
		})
	}
}

func TestProcessTimeToCutRequests(t *testing.T) {
	newBareMinimumChain := func(
		t *testing.T,
		mockSupport consensus.ConsenterSupport,
		hcsClient factory.HcsClient,
	) *chainImpl {
		startChan := make(chan struct{})
		close(startChan)
		errorChan := make(chan struct{})
		close(errorChan)
		haltChan := make(chan struct{})

		mockConsenter := &consenterImpl{
			sharedHcsConfigVal: &mockLocalConfig.Hcs,
			identityVal:        make([]byte, 16),
			metrics:            newFakeMetrics(newFakeMetricsFields()),
		}

		topicID := &hedera.TopicID{
			Shard: 0,
			Realm: 0,
			Topic: 16381,
		}

		privateKey, err := hedera.PrivateKeyFromString(testOperatorPrivateKey)
		assert.NoError(t, err, "Expected valid ed25519 private key")
		publicKey := privateKey.PublicKey()

		chain := &chainImpl{
			consenter:        mockConsenter,
			ConsenterSupport: mockSupport,

			topicID:   topicID,
			hcsClient: hcsClient,
			operatorID: &hedera.AccountID{
				Shard:   0,
				Realm:   0,
				Account: 16691,
			},
			operatorPrivateKey:     &privateKey,
			operatorPublicKeyBytes: publicKey.Bytes(),
			publicKeys: map[string]*hedera.PublicKey{
				string(publicKey.Bytes()): &publicKey,
			},

			startChan:              startChan,
			errorChan:              errorChan,
			haltChan:               haltChan,
			doneProcessingMessages: make(chan struct{}),
			timeToCutRequestChan:   make(chan timeToCutRequest),

			appID: []byte("bare-minimum appID"),
		}
		chain.appMsgProcessor, err = newAppMsgProcessor(testAccountID, chain.appID, maxConsensusMessageSize, normalReassembleTimeout, chain)
		assert.NoError(t, err, "Expected newAppMsgProcessor return no error")

		return chain
	}

	mockSupport := &mockmultichannel.ConsenterSupport{
		ChannelIDVal:     channelNameForTest(t),
		SharedConfigVal:  newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, nil),
		ChannelConfigVal: newMockChannel(),
	}

	t.Run("Proper", func(t *testing.T) {
		hcsClient := &mockhcs.HcsClient{}
		chain := newBareMinimumChain(t, mockSupport, hcsClient)

		done := make(chan struct{})
		go func() {
			chain.processTimeToCutRequests()
			done <- struct{}{}
		}()

		// closing doneProcessingMessages will cause processTimeToCutRequests() to exit
		close(chain.doneProcessingMessages)
		select {
		case <-done:
		case <-time.After(shortTimeout):
			t.Fatal("expected processTimeToCutRequests returned")
		}

		select {
		case _, ok := <-chain.timeToCutRequestChan:
			assert.False(t, ok, "expected timeToCutRequestChan closed")
		default:
			t.Fatal("expected receive from timeToCutRequestChan succeeded")
		}

		assert.Equal(t, 1, hcsClient.CloseCallCount(), "expected hcsClient closed")
	})

	t.Run("ProperWithMessageSent", func(t *testing.T) {
		syncCh := make(chan struct{})
		hcsClient := &mockhcs.HcsClient{}
		hcsClient.SubmitConsensusMessageCalls(func(_ []byte, _ *hedera.TopicID, txID *hedera.TransactionID) (*hedera.TransactionID, error) {
			syncCh <- struct{}{}
			if txID == nil {
				txID = &hedera.TransactionID{}
			}
			return txID, nil
		})
		chain := newBareMinimumChain(t, mockSupport, hcsClient)

		done := make(chan struct{})
		go func() {
			chain.processTimeToCutRequests()
			done <- struct{}{}
		}()

		dummyHash := []byte("dummy hash")
		chain.timeToCutRequestChan <- timeToCutRequest{
			messageHash: dummyHash,
		}

		select {
		case <-syncCh:
		case <-time.After(shortTimeout):
			t.Fatal("expected SubmitConsensusMessage called")
		}

		data, _, _ := hcsClient.SubmitConsensusMessageArgsForCall(0)
		chunk := &hb.ApplicationMessageChunk{}
		assert.NoError(t, proto.Unmarshal(data, chunk), "expected data successfully unmarshalled to ApplicationMessageChunk")
		rawHcsMsg, _, _, _, err := chain.appMsgProcessor.Reassemble(chunk, time.Now())
		assert.NoError(t, err, "expected reassemble successfully")
		hcsMsg := &hb.HcsMessage{}
		assert.NoError(t, proto.Unmarshal(rawHcsMsg, hcsMsg), "expected rawHcsMsg unmarshalled successfully")
		assert.IsType(t, &hb.HcsMessage_TimeToCut{}, hcsMsg.Type, "expected hcsMsg type is time-to-cut")
		assert.IsType(t, &hb.HcsMessageTimeToCut_MessageHash{}, hcsMsg.GetTimeToCut().Request, "expected time-to-cut type is MessageHash")
		assert.Equal(t, dummyHash, hcsMsg.GetTimeToCut().GetMessageHash(), "expected message hash equal")

		// closing doneProcessingMessages will cause processTimeToCutRequests() to exit
		close(chain.doneProcessingMessages)
		select {
		case <-done:
		case <-time.After(shortTimeout):
			t.Fatal("expected processTimeToCutRequests returned")
		}

		select {
		case _, ok := <-chain.timeToCutRequestChan:
			assert.False(t, ok, "expected timeToCutRequestChan closed")
		default:
			t.Fatal("expected receive from timeToCutRequestChan succeeded")
		}

		assert.Equal(t, 1, hcsClient.CloseCallCount(), "expected hcsClient closed")
	})

	t.Run("ProperWithMessageReceived", func(t *testing.T) {
		syncCh := make(chan struct{})
		hcsClient := &mockhcs.HcsClient{}
		hcsClient.SubmitConsensusMessageCalls(func(_ []byte, _ *hedera.TopicID, txID *hedera.TransactionID) (*hedera.TransactionID, error) {
			syncCh <- struct{}{}
			if txID == nil {
				txID = &hedera.TransactionID{}
			}
			return txID, nil
		})
		chain := newBareMinimumChain(t, mockSupport, hcsClient)

		done := make(chan struct{})
		go func() {
			chain.processTimeToCutRequests()
			done <- struct{}{}
		}()

		nextBlockNumber := uint64(5)
		chain.timeToCutRequestChan <- timeToCutRequest{
			start:           true,
			nextBlockNumber: nextBlockNumber,
		}

		select {
		case <-syncCh:
		case <-time.After(shortTimeout):
			t.Fatal("expected SubmitConsensusMessage called")
		}

		data, _, _ := hcsClient.SubmitConsensusMessageArgsForCall(0)
		chunk := &hb.ApplicationMessageChunk{}
		assert.NoError(t, proto.Unmarshal(data, chunk), "expected data successfully unmarshalled to ApplicationMessageChunk")
		rawHcsMsg, _, _, _, err := chain.appMsgProcessor.Reassemble(chunk, time.Now())
		assert.NoError(t, err, "expected reassemble successfully")
		hcsMsg := &hb.HcsMessage{}
		assert.NoError(t, proto.Unmarshal(rawHcsMsg, hcsMsg), "expected rawHcsMsg unmarshalled successfully")
		assert.IsType(t, &hb.HcsMessage_TimeToCut{}, hcsMsg.Type, "expected hcsMsg type is time-to-cut")
		assert.IsType(t, &hb.HcsMessageTimeToCut_BlockNumber{}, hcsMsg.GetTimeToCut().Request, "expected time-to-cut type is BlockNumber")
		assert.Equal(t, nextBlockNumber, hcsMsg.GetTimeToCut().GetBlockNumber(), "expected blockNumber equal")

		// closing doneProcessingMessages will cause processTimeToCutRequests() to exit
		close(chain.doneProcessingMessages)
		select {
		case <-done:
		case <-time.After(shortTimeout):
			t.Fatal("expected processTimeToCutRequests returned")
		}

		select {
		case _, ok := <-chain.timeToCutRequestChan:
			assert.False(t, ok, "expected timeToCutRequestChan closed")
		default:
			t.Fatal("expected receive from timeToCutRequestChan succeeded")
		}

		assert.Equal(t, 1, hcsClient.CloseCallCount(), "expected hcsClient closed")
	})

	t.Run("ProperWithStopRequested", func(t *testing.T) {
		syncCh := make(chan struct{})
		hcsClient := &mockhcs.HcsClient{}
		hcsClient.SubmitConsensusMessageCalls(func(_ []byte, _ *hedera.TopicID, txID *hedera.TransactionID) (*hedera.TransactionID, error) {
			syncCh <- struct{}{}
			if txID == nil {
				txID = &hedera.TransactionID{}
			}
			return txID, nil
		})

		chain := newBareMinimumChain(t, mockSupport, hcsClient)

		done := make(chan struct{})
		go func() {
			chain.processTimeToCutRequests()
			done <- struct{}{}
		}()

		nextBlockNumber := uint64(5)
		chain.timeToCutRequestChan <- timeToCutRequest{
			start:           true,
			nextBlockNumber: nextBlockNumber,
		}

		<-time.After(shortTimeout / 4)
		chain.timeToCutRequestChan <- timeToCutRequest{start: false}

		select {
		case <-syncCh:
			t.Fatal("expected no time-to-cut message sent")
		case <-time.After(shortTimeout / 2):
			// nothing
		}

		// closing doneProcessingMessages will cause processTimeToCutRequests() to exit
		close(chain.doneProcessingMessages)
		select {
		case <-done:
		case <-time.After(shortTimeout):
			t.Fatal("expected processTimeToCutRequests returned")
		}

		select {
		case _, ok := <-chain.timeToCutRequestChan:
			assert.False(t, ok, "expected timeToCutRequestChan closed")
		default:
			t.Fatal("expected receive from timeToCutRequestChan succeeded")
		}

		assert.Equal(t, 1, hcsClient.CloseCallCount(), "expected hcsClient closed")
	})
}

func TestProcessMessages(t *testing.T) {
	newBareMinimumChain := func(
		t *testing.T,
		lastCutBlockNumber uint64,
		mockSupport consensus.ConsenterSupport,
		hcf *hcsClientFactoryWithRecord,
		lastConsensusTimestampPersisted *time.Time,
		lastChunkFreeConsensusTimestamp *time.Time,
	) *chainImpl {
		errorChan := make(chan struct{})
		close(errorChan)
		haltChan := make(chan struct{})

		mockConsenter := &consenterImpl{
			sharedHcsConfigVal: &mockLocalConfig.Hcs,
			identityVal:        make([]byte, 16),
			metrics:            newFakeMetrics(newFakeMetricsFields()),
		}

		hcsClient, _ := hcf.GetHcsClient(nil, "", nil, nil)
		assert.NotNil(t, hcsClient, "Expected hcsClient created successfully")
		topicID := &hedera.TopicID{
			Shard: 0,
			Realm: 0,
			Topic: 16381,
		}
		topicSubscriptionHandle, _ := hcsClient.SubscribeTopic(topicID, &unixEpoch, nil)
		assert.NotNil(t, topicSubscriptionHandle, "Expected topic subscription handle created successfully")

		privateKey, err := hedera.PrivateKeyFromString(testOperatorPrivateKey)
		assert.NoError(t, err, "Expected valid ed25519 private key")
		publicKey := privateKey.PublicKey()

		subscriptionRetryTimer := time.NewTimer(time.Millisecond)
		if !subscriptionRetryTimer.Stop() {
			<-subscriptionRetryTimer.C
		}
		chain := &chainImpl{
			consenter:        mockConsenter,
			ConsenterSupport: mockSupport,

			lastCutBlockNumber: lastCutBlockNumber,

			topicID:                 topicID,
			hcsClient:               hcsClient,
			topicSubscriptionHandle: topicSubscriptionHandle,
			operatorPrivateKey:      &privateKey,
			operatorPublicKeyBytes:  publicKey.Bytes(),
			publicKeys: map[string]*hedera.PublicKey{
				string(publicKey.Bytes()): &publicKey,
			},

			subscriptionRetryTimer: subscriptionRetryTimer,
			errorChan:              errorChan,
			haltChan:               haltChan,
			doneProcessingMessages: make(chan struct{}),
			timeToCutRequestChan:   make(chan timeToCutRequest),
			messageHashes:          make(map[string]struct{}),
			appID:                  []byte("bare-minimum appID"),
		}
		chain.appMsgProcessor, err = newAppMsgProcessor(testAccountID, chain.appID, maxConsensusMessageSize, normalReassembleTimeout, chain)
		assert.NoError(t, err, "Expected newAppMsgProcessor return no error")

		if lastConsensusTimestampPersisted != nil {
			chain.lastConsensusTimestampPersisted = *lastConsensusTimestampPersisted
		}
		if lastChunkFreeConsensusTimestamp != nil {
			chain.lastChunkFreeConsensusTimestamp = *lastChunkFreeConsensusTimestamp
		}
		return chain
	}
	var err error

	t.Run("TimeToCut", func(t *testing.T) {
		t.Run("PendingMsgToCutProper", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:   mockblockcutter.NewReceiver(),
				Blocks:           make(chan *cb.Block),
				ChannelIDVal:     channelNameForTest(t),
				SharedConfigVal:  newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, publicKeys),
				ChannelConfigVal: newMockChannel(),
			}
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, mockSupport, hcf, nil, nil)

			done := make(chan struct{})
			go chain.processTimeToCutRequests()
			go func() {
				err = chain.processMessages()
				done <- struct{}{}
			}()
			close(mockSupport.BlockCutterVal.Block)
			close(getRespSyncChan(chain.topicSubscriptionHandle))

			// plant a message directly to the mock blockcutter
			mockSupport.BlockCutterVal.Ordered(newMockEnvelope("foo message"))

			// cut ancestors
			mockSupport.BlockCutterVal.CutAncestors = true

			hcsMessage := newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelope("foo message 2")), uint64(0), uint64(0))
			chunks, _, err1 := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(hcsMessage), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err1, "Expected Split returns no error")
			chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)

			// wait for the first block
			select {
			case <-mockSupport.Blocks:
			case <-time.After(shortTimeout):
				t.Fatal("Expected one block cut")
			}

			logger.Debug("closing haltChan to exit processMessages")
			close(chain.haltChan) // cause processMessages to exit
			logger.Debug("haltChan closed")
			<-done

			assert.NoError(t, err, "Expected processMessages to exit without errors")
			assert.NotEmpty(t, mockSupport.BlockCutterVal.CurBatch(), "Expected the blockcutter to be non-empty")
		})

		t.Run("ReceiveTimeToCutProper", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:   mockblockcutter.NewReceiver(),
				Blocks:           make(chan *cb.Block),
				ChannelIDVal:     channelNameForTest(t),
				SharedConfigVal:  newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, publicKeys),
				ChannelConfigVal: newMockChannel(),
			}
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, mockSupport, hcf, nil, nil)
			done := make(chan struct{})

			go chain.processTimeToCutRequests()
			go func() {
				err = chain.processMessages()
				done <- struct{}{}
			}()
			close(mockSupport.BlockCutterVal.Block)
			close(getRespSyncChan(chain.topicSubscriptionHandle))

			// plant a message directly to the mock blockcutter
			mockSupport.BlockCutterVal.Ordered(newMockEnvelope("foo message"))

			msg := newTimeToCutMessageWithBlockNumber(lastCutBlockNumber + 1)
			chunks, _, err1 := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err1, "Expected Split returns no error")
			chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)

			// wait for the first block
			select {
			case <-mockSupport.Blocks:
			case <-time.After(shortTimeout):
				t.Fatal("Expected one block cut")
			}

			logger.Debug("closing haltChan to exit processMessages")
			close(chain.haltChan) // cause processMessages to exit
			logger.Debug("haltChan closed")
			<-done

			assert.NoError(t, err, "Expected the processMessages call to return without errors")
			assert.Equal(t, lastCutBlockNumber+1, chain.lastCutBlockNumber, "Expected lastCutBlockNumber to be increased by one")
		})

		t.Run("ReceiveTimeToCutZeroBatch", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:   mockblockcutter.NewReceiver(),
				Blocks:           make(chan *cb.Block),
				ChannelIDVal:     channelNameForTest(t),
				SharedConfigVal:  newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, publicKeys),
				ChannelConfigVal: newMockChannel(),
			}
			defer close(mockSupport.BlockCutterVal.Block)
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, mockSupport, hcf, nil, nil)
			done := make(chan struct{})
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)

			go chain.processTimeToCutRequests()
			go func() {
				err = chain.processMessages()
				done <- struct{}{}
			}()

			msg := newTimeToCutMessageWithBlockNumber(lastCutBlockNumber + 1)
			chunks, _, err1 := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err1, "Expected Split returns no error")
			chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			respSyncChan <- struct{}{} // sync with subscription handle to ensure the message is received by processMessages

			logger.Debug("closing haltChan to exit processMessages")
			close(chain.haltChan) // cause processMessages to exit
			logger.Debug("haltChan closed")
			<-done

			assert.Error(t, err, "Expected the processMessages call to return errors")
			assert.Equal(t, lastCutBlockNumber, chain.lastCutBlockNumber, "Expected lastCutBlockNumber to stay the same")
		})

		t.Run("ReceiveTimeToCutLargerThanExpected", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:   mockblockcutter.NewReceiver(),
				Blocks:           make(chan *cb.Block),
				ChannelIDVal:     channelNameForTest(t),
				SharedConfigVal:  newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, publicKeys),
				ChannelConfigVal: newMockChannel(),
			}
			defer close(mockSupport.BlockCutterVal.Block)
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, mockSupport, hcf, nil, nil)
			done := make(chan struct{})
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)

			go chain.processTimeToCutRequests()
			go func() {
				err = chain.processMessages()
				done <- struct{}{}
			}()

			// larger than expected block number,
			msg := newTimeToCutMessageWithBlockNumber(lastCutBlockNumber + 2)
			chunks, _, err1 := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err1, "Expected Split returns no error")
			chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			respSyncChan <- struct{}{} // sync with subscription handle to ensure the message is received by processMessages

			logger.Debug("closing haltChan to exit processMessages")
			close(chain.haltChan) // cause processMessages to exit
			logger.Debug("haltChan closed")
			<-done

			assert.Error(t, err, "Expected the processMessages call to return errors")
			assert.Equal(t, lastCutBlockNumber, chain.lastCutBlockNumber, "Expected lastCutBlockNumber to stay the same")
		})

		t.Run("ReceiveTimeToCutStale", func(t *testing.T) {
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:   mockblockcutter.NewReceiver(),
				Blocks:           make(chan *cb.Block),
				ChannelIDVal:     channelNameForTest(t),
				SharedConfigVal:  newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, publicKeys),
				ChannelConfigVal: newMockChannel(),
			}
			lastCutBlockNumber := uint64(3)
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, mockSupport, hcf, nil, nil)
			done := make(chan struct{})
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)

			go chain.processTimeToCutRequests()
			go func() {
				err = chain.processMessages()
				done <- struct{}{}
			}()
			close(mockSupport.BlockCutterVal.Block)

			// larger than expected block number,
			msg := newTimeToCutMessageWithBlockNumber(lastCutBlockNumber)
			chunks, _, err1 := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.NoError(t, err1, "Expected Split returns no error")
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			respSyncChan <- struct{}{}

			logger.Debug("closing haltChan to exit processMessages")
			close(chain.haltChan) // cause processMessages to exit
			logger.Debug("haltChan closed")
			<-done

			assert.NoError(t, err, "Expected the processMessages call to return without errors")
			assert.Equal(t, lastCutBlockNumber, chain.lastCutBlockNumber, "Expected lastCutBlockNumber to stay the same")
		})
	})

	t.Run("Regular", func(t *testing.T) {
		t.Run("Error", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:   mockblockcutter.NewReceiver(),
				Blocks:           make(chan *cb.Block),
				ChannelIDVal:     channelNameForTest(t),
				SharedConfigVal:  newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, publicKeys),
				ChannelConfigVal: newMockChannel(),
			}
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, mockSupport, hcf, nil, nil)
			done := make(chan struct{})
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)

			go chain.processTimeToCutRequests()
			go func() {
				err = chain.processMessages()
				done <- struct{}{}
			}()
			close(mockSupport.BlockCutterVal.Block)

			msg := newNormalMessage([]byte("bytes won't unmarshal to envelope"), uint64(0), uint64(0))
			chunks, _, err1 := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err1, "Expected Split returns no error")
			chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			respSyncChan <- struct{}{}

			logger.Debug("closing haltChan to exit processMessages")
			close(chain.haltChan) // cause processMessages to exit
			logger.Debug("haltChan closed")
			<-done

			assert.NoError(t, err, "Expected the processMessages call to return without errors")
			assert.Empty(t, mockSupport.BlockCutterVal.CurBatch(), "Expect no message committed to blockcutter")
		})

		t.Run("Normal", func(t *testing.T) {
			t.Run("ReceiveTwoRegularAndCutTwoBlocks", func(t *testing.T) {
				lastCutBlockNumber := uint64(3)
				publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
				mockSupport := &mockmultichannel.ConsenterSupport{
					BlockCutterVal:   mockblockcutter.NewReceiver(),
					Blocks:           make(chan *cb.Block),
					ChannelIDVal:     channelNameForTest(t),
					SharedConfigVal:  newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, publicKeys),
					ChannelConfigVal: newMockChannel(),
				}
				hcf := newDefaultMockHcsClientFactory()
				chain := newBareMinimumChain(t, lastCutBlockNumber, mockSupport, hcf, nil, nil)
				done := make(chan struct{})
				defer close(mockSupport.BlockCutterVal.Block)
				respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
				defer close(respSyncChan)

				go chain.processTimeToCutRequests()
				go func() {
					err = chain.processMessages()
					done <- struct{}{}
				}()

				// first message
				msg := newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelope("test message 1")), uint64(0), uint64(0))
				chunks, _, err1 := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
				assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
				assert.NoError(t, err1, "Expected Split returns no error")
				block1ProtoTimestamp := timestampProtoOrPanic(getNextConsensusTimestamp(chain.topicSubscriptionHandle))
				chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
				mockSupport.BlockCutterVal.Block <- struct{}{}
				respSyncChan <- struct{}{}

				mockSupport.BlockCutterVal.IsolatedTx = true

				// second message
				msg = newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelope("test message 2")), uint64(0), uint64(0))
				chunks, _, err1 = chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
				assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
				assert.NoError(t, err1, "Expected Split returns no error")
				block2ProtoTimestamp := timestampProtoOrPanic(getNextConsensusTimestamp(chain.topicSubscriptionHandle))
				chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
				mockSupport.BlockCutterVal.Block <- struct{}{}
				respSyncChan <- struct{}{}

				var block1, block2 *cb.Block
				select {
				case block1 = <-mockSupport.Blocks:
				case <-time.After(shortTimeout):
					t.Fatalf("Did not receive the first block from the blockcutter as expected")
				}

				select {
				case block2 = <-mockSupport.Blocks:
				case <-time.After(shortTimeout):
					t.Fatalf("Did not receive the second block from the blockcutter as expected")
				}

				logger.Debug("Closing haltChan to exit the infinite for-loop")
				close(chain.haltChan) // Identical to chain.Halt()
				logger.Debug("haltChan closed")
				<-done

				assert.NoError(t, err, "Expected the procesMessages call to return without errors")
				assert.Equal(t, lastCutBlockNumber+2, chain.lastCutBlockNumber, "Expected 2 blocks cut")
				assert.Equal(t, block1ProtoTimestamp, extractConsensusTimestamp(block1), "Expected encoded offset in first block to correct")
				assert.Equal(t, block2ProtoTimestamp, extractConsensusTimestamp(block2), "Expected encoded offset in second block to correct")
			})

			t.Run("ReceiveRegularAndQueue", func(t *testing.T) {
				lastCutBlockNumber := uint64(3)
				publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
				mockSupport := &mockmultichannel.ConsenterSupport{
					BlockCutterVal:   mockblockcutter.NewReceiver(),
					Blocks:           make(chan *cb.Block),
					ChannelIDVal:     channelNameForTest(t),
					SharedConfigVal:  newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, publicKeys),
					ChannelConfigVal: newMockChannel(),
				}
				hcf := newDefaultMockHcsClientFactory()
				chain := newBareMinimumChain(t, lastCutBlockNumber, mockSupport, hcf, nil, nil)
				done := make(chan struct{})
				close(mockSupport.BlockCutterVal.Block)
				close(getRespSyncChan(chain.topicSubscriptionHandle))

				go chain.processTimeToCutRequests()
				go func() {
					err = chain.processMessages()
					done <- struct{}{}
				}()

				mockSupport.BlockCutterVal.CutNext = true

				msg := newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelope("test message")), uint64(0), uint64(0))
				chunks, _, err1 := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
				assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
				assert.NoError(t, err1, "Expected Split returns no error")
				chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
				<-mockSupport.Blocks

				close(chain.haltChan)
				logger.Debug("haltChan closed")
				<-done

				assert.NoError(t, err, "Expected the processMessages call to return without errors")
			})
		})

		// this ensures CONFIG messages are handled properly
		t.Run("Config", func(t *testing.T) {
			// a normal tx followed by a config tx, should yield two blocks
			t.Run("ReceiveConfigEnvelopeAndCut", func(t *testing.T) {
				lastCutBlockNumber := uint64(3)
				publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
				mockSupport := &mockmultichannel.ConsenterSupport{
					BlockCutterVal:   mockblockcutter.NewReceiver(),
					Blocks:           make(chan *cb.Block),
					ChannelIDVal:     channelNameForTest(t),
					SharedConfigVal:  newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, publicKeys),
					ChannelConfigVal: newMockChannel(),
				}
				hcf := newDefaultMockHcsClientFactory()
				chain := newBareMinimumChain(t, lastCutBlockNumber, mockSupport, hcf, nil, nil)
				done := make(chan struct{})
				close(mockSupport.BlockCutterVal.Block)
				respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
				defer close(respSyncChan)

				go chain.processTimeToCutRequests()
				go func() {
					err = chain.processMessages()
					done <- struct{}{}
				}()

				// normal message
				msg := newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelope("test message")), uint64(0), uint64(0))
				chunks, _, err1 := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
				assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
				assert.NoError(t, err1, "Expected Split returns no error")
				normalBlockTimestamp := timestampProtoOrPanic(getNextConsensusTimestamp(chain.topicSubscriptionHandle))
				chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
				respSyncChan <- struct{}{}

				// config message
				msg = newConfigMessage(protoutil.MarshalOrPanic(newMockConfigEnvelope()), uint64(0), uint64(0))
				chunks, _, err1 = chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
				assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
				assert.NoError(t, err1, "Expected Split returns no error")
				configBlockTimestamp := timestampProtoOrPanic(getNextConsensusTimestamp(chain.topicSubscriptionHandle))
				chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
				respSyncChan <- struct{}{}

				var normalBlock, configBlock *cb.Block
				select {
				case normalBlock = <-mockSupport.Blocks:
				case <-time.After(shortTimeout):
					t.Fatalf("Did not receive a normal block from the blockcutter as expected")
				}

				select {
				case configBlock = <-mockSupport.Blocks:
				case <-time.After(shortTimeout):
					t.Fatalf("Did not receive a config block from the blockcutter as expected")
				}

				close(chain.haltChan)
				logger.Debug("haltChan closed")
				<-done

				assert.NoError(t, err, "Expected the processMessages call to return without errors")
				assert.Equal(t, lastCutBlockNumber+2, chain.lastCutBlockNumber, "Expected two blocks cut and writtern")
				assert.Equal(t, normalBlockTimestamp, extractConsensusTimestamp(normalBlock), "Expected correct consensus timestamp in normal block")
				assert.Equal(t, configBlockTimestamp, extractConsensusTimestamp(configBlock), "Expected correct consensus timestamp in config block")
			})

			t.Run("RevalidateConfigEnvInvalid", func(t *testing.T) {
				lastCutBlockNumber := uint64(3)
				publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
				mockSupport := &mockmultichannel.ConsenterSupport{
					BlockCutterVal:      mockblockcutter.NewReceiver(),
					Blocks:              make(chan *cb.Block),
					ChannelIDVal:        channelNameForTest(t),
					HeightVal:           lastCutBlockNumber,
					ClassifyMsgVal:      msgprocessor.ConfigMsg,
					SharedConfigVal:     newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, publicKeys),
					SequenceVal:         uint64(1), // config sequence 1
					ProcessConfigMsgErr: fmt.Errorf("invalid config message"),
					ChannelConfigVal:    newMockChannel(),
				}
				hcf := newDefaultMockHcsClientFactory()
				chain := newBareMinimumChain(t, lastCutBlockNumber, mockSupport, hcf, nil, nil)
				done := make(chan struct{})
				close(mockSupport.BlockCutterVal.Block)
				close(getRespSyncChan(chain.topicSubscriptionHandle))

				go chain.processTimeToCutRequests()
				go func() {
					err = chain.processMessages()
					done <- struct{}{}
				}()

				// config message with configseq 0
				msg := newConfigMessage(protoutil.MarshalOrPanic(newMockConfigEnvelope()), uint64(0), uint64(0))
				chunks, _, err1 := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
				assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
				assert.NoError(t, err1, "Expected Split returns no error")
				chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
				select {
				case <-mockSupport.Blocks:
					t.Fatal("Expected no block being cut given invalid config message")
				case <-time.After(shortTimeout):
					// do nothing
				}

				close(chain.haltChan)
				logger.Debug("haltChan closed")
				<-done

				assert.NoError(t, err, "Expected the processMessages call to return without errors")
			})
		})
	})

	t.Run("RecollectPendingChunks", func(t *testing.T) {
		t.Run("ReceiveMessageWithFutureConsensusTimestamp", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:   mockblockcutter.NewReceiver(),
				Blocks:           make(chan *cb.Block),
				ChannelIDVal:     channelNameForTest(t),
				SharedConfigVal:  newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, publicKeys),
				ChannelConfigVal: newMockChannel(),
			}
			hcf := newDefaultMockHcsClientFactory()
			lastConsensusTimestampPersisted := unixEpoch.Add(100 * time.Hour)
			lastChunkFreeConsensusTimestamp := lastConsensusTimestampPersisted.Add(-30 * time.Minute)
			chain := newBareMinimumChain(t, lastCutBlockNumber, mockSupport, hcf, &lastConsensusTimestampPersisted, &lastChunkFreeConsensusTimestamp)
			done := make(chan struct{})
			close(mockSupport.BlockCutterVal.Block)
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)

			go func() {
				assert.Panics(t, func() { chain.processMessages() }, "Expected panic if no message with matching consensus timestamp")
				done <- struct{}{}
			}()

			setNextConsensusTimestamp(chain.topicSubscriptionHandle, lastConsensusTimestampPersisted.Add(time.Nanosecond))
			msg := newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelope("test message")), uint64(0), uint64(0))
			var chunks []*hb.ApplicationMessageChunk
			chunks, _, err = chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err, "Expected Split returns no error")
			_, err = chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			assert.NoError(t, err, "Expect message sent successfully")
			respSyncChan <- struct{}{}

			close(chain.haltChan)
			logger.Debug("haltChan closed")
			<-done
		})
	})
}

func TestResubmission(t *testing.T) {
	blockIngressMsg := func(t *testing.T, block bool, fn func() error) {
		wait := make(chan struct{})
		go func() {
			fn()
			wait <- struct{}{}
		}()

		select {
		case <-wait:
			if block {
				t.Fatalf("Expected WaitReady to block")
			}
		case <-time.After(100 * time.Millisecond):
			if !block {
				t.Fatalf("Expected WaitReady not to block")
			}
		}
	}

	newBareMinimumChain := func(
		t *testing.T,
		lastCutBlockNumber uint64,
		lastOriginalSequenceProcessed uint64,
		inReprocessing bool,
		mockSupport consensus.ConsenterSupport,
		hcf *hcsClientFactoryWithRecord,
	) *chainImpl {
		startChan := make(chan struct{})
		close(startChan)
		errorChan := make(chan struct{})
		close(errorChan)
		haltChan := make(chan struct{})
		doneReprocessingMsgInFlight := make(chan struct{})
		if !inReprocessing {
			close(doneReprocessingMsgInFlight)
		}

		mockConsenter := &consenterImpl{
			sharedHcsConfigVal: &mockLocalConfig.Hcs,
			identityVal:        make([]byte, 16),
			metrics:            newFakeMetrics(newFakeMetricsFields()),
		}

		hcsClient, _ := hcf.GetHcsClient(nil, "", nil, nil)
		assert.NotNil(t, hcsClient, "Expected hcsClient created successfully")
		topicID := &hedera.TopicID{
			Shard: 0,
			Realm: 0,
			Topic: 16381,
		}
		topicSubscriptionHandle, _ := hcsClient.SubscribeTopic(topicID, &unixEpoch, nil)
		assert.NotNil(t, topicSubscriptionHandle, "Expected topic subscription handle created successfully")

		privateKey, err := hedera.PrivateKeyFromString(testOperatorPrivateKey)
		assert.NoError(t, err, "Expected valid ed25519 private key")
		publicKey := privateKey.PublicKey()

		subscriptionRetryTimer := time.NewTimer(time.Millisecond)
		if !subscriptionRetryTimer.Stop() {
			<-subscriptionRetryTimer.C
		}

		chain := &chainImpl{
			consenter:        mockConsenter,
			ConsenterSupport: mockSupport,

			lastOriginalSequenceProcessed: lastOriginalSequenceProcessed,
			lastCutBlockNumber:            lastCutBlockNumber,

			topicID:                 topicID,
			hcsClient:               hcsClient,
			topicSubscriptionHandle: topicSubscriptionHandle,
			operatorID: &hedera.AccountID{
				Shard:   0,
				Realm:   0,
				Account: 16591,
			},
			operatorPrivateKey:     &privateKey,
			operatorPublicKeyBytes: publicKey.Bytes(),
			publicKeys: map[string]*hedera.PublicKey{
				string(publicKey.Bytes()): &publicKey,
			},

			subscriptionRetryTimer:      subscriptionRetryTimer,
			startChan:                   startChan,
			errorChan:                   errorChan,
			haltChan:                    haltChan,
			doneProcessingMessages:      make(chan struct{}),
			timeToCutRequestChan:        make(chan timeToCutRequest),
			messageHashes:               make(map[string]struct{}),
			doneReprocessingMsgInFlight: doneReprocessingMsgInFlight,
			appID:                       []byte("bare-minimum appID"),
		}
		chain.appMsgProcessor, err = newAppMsgProcessor(testAccountID, chain.appID, maxConsensusMessageSize, normalReassembleTimeout, chain)
		assert.NoError(t, err, "Expected newAppMsgProcessor return no error")
		return chain
	}
	var processErr error

	t.Run("Normal", func(t *testing.T) {
		// this test emits a re-submitted message that does not require reprocessing
		// (by setting OriginalSequence < lastOriginalSequenceProcessed
		t.Run("AlreadyProcessedDiscard", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			lastOriginalSequenceProcessed := uint64(5)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:   mockblockcutter.NewReceiver(),
				Blocks:           make(chan *cb.Block),
				ChannelIDVal:     channelNameForTest(t),
				SharedConfigVal:  newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, publicKeys),
				ChannelConfigVal: newMockChannel(),
			}
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, lastOriginalSequenceProcessed, false, mockSupport, hcf)
			close(mockSupport.BlockCutterVal.Block)
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)
			done := make(chan struct{})

			go chain.processTimeToCutRequests()
			go func() {
				processErr = chain.processMessages()
				done <- struct{}{}
			}()

			mockSupport.BlockCutterVal.CutNext = true

			// normal message
			msg := newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelope("test message")), uint64(0), lastOriginalSequenceProcessed-1)
			chunks, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err, "Expect Split returns no error")
			_, err = chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			assert.NoError(t, err, "Expect message sent successfully")
			respSyncChan <- struct{}{}

			select {
			case <-mockSupport.Blocks:
				t.Fatal("Expected no block being cut")
			case <-time.After(shortTimeout):
				// do nothing
			}

			close(chain.haltChan)
			logger.Debug("haltChan closed")
			<-done

			assert.NoError(t, processErr, "Expected processMessages to return without errors")
		})

		// This test emits a mock re-submitted message that requires reprocessing
		// (by setting OriginalSequence > lastOriginalSequenceProcessed)
		// Two normal messages are enqueued in this test case: reprocessed normal message where
		// `originalOffset` is not 0, followed by a normal msg  where `OriginalSequence` is 0.
		// It tests the case that even no block is cut, `lastOriginalSequenceProcessed` is still
		// updated. We inspect the block to verify correct `lastOriginalSequenceProcessed` in the
		// hcs metadata.
		t.Run("ResubmittedMsgEnqueue", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			lastOriginalSequenceProcessed := uint64(5)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:   mockblockcutter.NewReceiver(),
				Blocks:           make(chan *cb.Block),
				ChannelIDVal:     channelNameForTest(t),
				HeightVal:        lastCutBlockNumber,
				SharedConfigVal:  newMockOrderer(longTimeout, goodHcsTopicIDStr, publicKeys),
				SequenceVal:      uint64(0),
				ChannelConfigVal: newMockChannel(),
			}
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, lastOriginalSequenceProcessed, false, mockSupport, hcf)
			defer close(mockSupport.BlockCutterVal.Block)
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)
			done := make(chan struct{})

			go chain.processTimeToCutRequests()
			go func() {
				processErr = chain.processMessages()
				done <- struct{}{}
			}()

			// normal message which advances lastOriginalSequenceProcessed
			msg := newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelope("test message")), uint64(0), lastOriginalSequenceProcessed+1)
			chunks, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err, "Expect Split returns no error")
			_, err = chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			assert.NoError(t, err, "Expect message sent successfully")
			mockSupport.BlockCutterVal.Block <- struct{}{}
			respSyncChan <- struct{}{} // sync to make sure the message is received by processMessages

			select {
			case <-mockSupport.Blocks:
				t.Fatalf("Expected no block to be cut")
			case <-time.After(shortTimeout):
			}

			mockSupport.BlockCutterVal.CutNext = true
			msg = newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelope("test message")), uint64(0), uint64(0))
			chunks, _, err = chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err, "Expect Split returns no error")
			_, err = chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			assert.NoError(t, err, "Expect message sent successfully")
			mockSupport.BlockCutterVal.Block <- struct{}{}
			respSyncChan <- struct{}{} // sync to make sure the message is received by processMessages

			select {
			case block := <-mockSupport.Blocks:
				metadata := &cb.Metadata{}
				proto.Unmarshal(block.Metadata.Metadata[cb.BlockMetadataIndex_ORDERER], metadata)
				hcsMetadata := &hb.HcsMetadata{}
				proto.Unmarshal(metadata.Value, hcsMetadata)
				assert.Equal(t, lastOriginalSequenceProcessed+1, hcsMetadata.LastOriginalSequenceProcessed)
			case <-time.After(shortTimeout):
				t.Fatal("Expected on block being cut")
			}

			close(chain.haltChan)
			logger.Debug("haltChan closed")
			<-done

			assert.NoError(t, processErr, "Expected the processMessages call to return without errors")
		})

		t.Run("InvalidDiscard", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			lastOriginalSequenceProcessed := uint64(5)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:      mockblockcutter.NewReceiver(),
				Blocks:              make(chan *cb.Block),
				ChannelIDVal:        channelNameForTest(t),
				HeightVal:           lastCutBlockNumber,
				SharedConfigVal:     newMockOrderer(shortTimeout/2, goodHcsTopicIDStr, publicKeys),
				SequenceVal:         uint64(1),
				ProcessNormalMsgErr: fmt.Errorf("invalid normal message"),
				ChannelConfigVal:    newMockChannel(),
			}
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, lastOriginalSequenceProcessed, false, mockSupport, hcf)
			defer close(mockSupport.BlockCutterVal.Block)
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)
			done := make(chan struct{})

			go chain.processTimeToCutRequests()
			go func() {
				processErr = chain.processMessages()
				done <- struct{}{}
			}()

			// config message which old configSeq, should try resubmit and receive error as message is invalidated
			msg := newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelope("test message")), uint64(0), uint64(0))
			chunks, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err, "Expect Split returns no error")
			_, err = chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			assert.NoError(t, err, "Expect message sent successfully")
			respSyncChan <- struct{}{} // sync to make sure the message is received by processMessages

			select {
			case mockSupport.BlockCutterVal.Block <- struct{}{}:
				t.Fatalf("Expected no message committed to blockcutter")
			case <-mockSupport.Blocks:
				t.Fatalf("Expected no block being cut given invalid config message")
			case <-time.After(shortTimeout):
			}

			close(chain.haltChan)
			logger.Debug("haltChan closed")
			<-done

			assert.NoError(t, processErr, "Expected processMessages call to return without errors")
		})

		t.Run("ValidResubmit", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			lastOriginalSequenceProcessed := uint64(0)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:   mockblockcutter.NewReceiver(),
				Blocks:           make(chan *cb.Block),
				ChannelIDVal:     channelNameForTest(t),
				HeightVal:        lastCutBlockNumber,
				SharedConfigVal:  newMockOrderer(longTimeout, goodHcsTopicIDStr, publicKeys),
				SequenceVal:      uint64(1),
				ConfigSeqVal:     uint64(1),
				ChannelConfigVal: newMockChannel(),
			}
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, lastOriginalSequenceProcessed, false, mockSupport, hcf)
			close(mockSupport.BlockCutterVal.Block)
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)
			done := make(chan struct{})

			go chain.processTimeToCutRequests()
			go func() {
				processErr = chain.processMessages()
				done <- struct{}{}
			}()

			// should cut one block after re-submitted message is processed
			mockSupport.BlockCutterVal.CutNext = true

			// config message with old configSeq, should try resubmit
			sequence := getNextSequenceNumber(chain.topicSubscriptionHandle)
			msg := newNormalMessage(protoutil.MarshalOrPanic(newMockEnvelope("test message")), uint64(0), uint64(0))
			chunks, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err, "Expect Split returns no error")
			_, err = chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			assert.NoError(t, err, "Expected message injected successfully")

			select {
			case <-mockSupport.Blocks:
				t.Fatalf("Expected no block being cut given message with old configSeq")
			case <-time.After(shortTimeout):
			}

			// WaitReady should not block
			blockIngressMsg(t, false, chain.WaitReady)
			respSyncChan <- struct{}{} // sync to make sure the message is received by processMessages, and unblock the resubmitted message

			select {
			case respSyncChan <- struct{}{}:
			case <-time.After(100 * time.Millisecond):
				t.Fatalf("Expected message is resubmitted")
			}

			mockHcsClient := chain.hcsClient.(*mockhcs.HcsClient)
			assert.Equal(t, 2, mockHcsClient.SubmitConsensusMessageCallCount(), "Expect SubmitConsensusMessage called once")
			marshalledMsg, _, _ := mockHcsClient.SubmitConsensusMessageArgsForCall(1)
			chunk := &hb.ApplicationMessageChunk{}
			assert.NoError(t, proto.Unmarshal(marshalledMsg, chunk), "Expected data unmarshalled successfully to ApplicationMessageChunk")
			appMsg := &hb.ApplicationMessage{}
			assert.NoError(t, proto.Unmarshal(chunk.MessageChunk, appMsg), "Expected data unmarshalled successfully to ApplicationMessage")
			hcsMessage := &hb.HcsMessage{}
			assert.NoError(t, proto.Unmarshal(appMsg.BusinessProcessMessage, hcsMessage), "Expected data unmarshalled successfully to HcsMessage")
			normalMessage := hcsMessage.Type.(*hb.HcsMessage_Regular).Regular
			assert.Equal(t, mockSupport.ConfigSeqVal, normalMessage.ConfigSeq, "Expect configseq to be current")
			assert.Equal(t, sequence, normalMessage.OriginalSeq, "Expect originalSeq to match")

			select {
			case <-mockSupport.Blocks:
			case <-time.After(100 * time.Millisecond):
				t.Fatalf("Expected one block being cut")
			}
			close(chain.haltChan)
			logger.Debug("haltChan closed")
			<-done

			assert.NoError(t, processErr, "Expected processMessages call to return without errors")
		})
	})

	t.Run("Config", func(t *testing.T) {
		// this test emits a mock re-submitted config message that does not require reprocessing as
		// OriginalSequence <= lastOriginalSequenceProcessed
		t.Run("AlreadyProcessedDiscard", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			lastOriginalSequenceProcessed := uint64(5)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:   mockblockcutter.NewReceiver(),
				Blocks:           make(chan *cb.Block),
				ChannelIDVal:     channelNameForTest(t),
				HeightVal:        lastCutBlockNumber,
				SharedConfigVal:  newMockOrderer(longTimeout, goodHcsTopicIDStr, publicKeys),
				SequenceVal:      uint64(1),
				ConfigSeqVal:     uint64(1),
				ChannelConfigVal: newMockChannel(),
			}
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, lastOriginalSequenceProcessed, false, mockSupport, hcf)
			close(mockSupport.BlockCutterVal.Block)
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)
			done := make(chan struct{})

			go chain.processTimeToCutRequests()
			go func() {
				processErr = chain.processMessages()
				done <- struct{}{}
			}()

			// config message with configseq 0
			msg := newConfigMessage(protoutil.MarshalOrPanic(newMockConfigEnvelope()), uint64(0), lastOriginalSequenceProcessed-1)
			chunks, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err, "Expect Split returns no error")
			_, err = chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			assert.NoError(t, err, "Expect message sent successfully")
			respSyncChan <- struct{}{}

			select {
			case <-mockSupport.Blocks:
				t.Fatalf("Expected no block cut")
			case <-time.After(shortTimeout / 2):
			}

			close(chain.haltChan)
			logger.Debug("haltChan closed")
			<-done

			assert.NoError(t, processErr, "Expected processMessages call to return without errors")
		})

		// scenario, some other orderer resubmitted message at offset X, whereas we didn't. That message was considered
		// invalid by us during re-validation, however some other orderer deemed it to be valid, and thus resubmitted it
		t.Run("Non-determinism", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			lastOriginalSequenceProcessed := uint64(4)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:      mockblockcutter.NewReceiver(),
				Blocks:              make(chan *cb.Block),
				ChannelIDVal:        channelNameForTest(t),
				HeightVal:           lastCutBlockNumber,
				SharedConfigVal:     newMockOrderer(longTimeout, goodHcsTopicIDStr, publicKeys),
				SequenceVal:         uint64(1),
				ConfigSeqVal:        uint64(1),
				ProcessConfigMsgVal: newMockConfigEnvelope(),
				ChannelConfigVal:    newMockChannel(),
			}
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, lastOriginalSequenceProcessed, false, mockSupport, hcf)
			close(mockSupport.BlockCutterVal.Block)
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)
			done := make(chan struct{})

			go chain.processTimeToCutRequests()
			go func() {
				processErr = chain.processMessages()
				done <- struct{}{}
			}()

			// check that WaitReady is not blocked at beginning
			blockIngressMsg(t, false, chain.WaitReady)

			mockSupport.ProcessConfigMsgErr = fmt.Errorf("invalid message found during revalidation")

			// emits a config message with lagged config sequence
			msg := newConfigMessage(protoutil.MarshalOrPanic(newMockConfigEnvelope()), uint64(0), uint64(0))
			chunks, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err, "Expect Split returns no error")
			_, err = chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			assert.NoError(t, err, "Expect message sent successfully")
			respSyncChan <- struct{}{}
			select {
			case <-mockSupport.Blocks:
				t.Fatalf("Expected no block being cut")
			case <-time.After(shortTimeout / 2):
			}

			// check that WaitReady is still not blocked
			blockIngressMsg(t, false, chain.WaitReady)

			// some other orderer resubmitted the message
			// emits a config message with lagged config sequence
			msg = newConfigMessage(protoutil.MarshalOrPanic(newMockConfigEnvelope()), mockSupport.SequenceVal, lastOriginalSequenceProcessed+1)
			chunks, _, err = chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err, "Expect Split returns no error")
			_, err = chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			assert.NoError(t, err, "Expect message sent successfully")
			respSyncChan <- struct{}{}

			select {
			case block := <-mockSupport.Blocks:
				metadata, err := protoutil.GetMetadataFromBlock(block, cb.BlockMetadataIndex_ORDERER)
				assert.NoError(t, err, "Expected get metadata from block successful")
				hcsMetadata := &hb.HcsMetadata{}
				assert.NoError(t, proto.Unmarshal(metadata.Value, hcsMetadata), "Expected unmarsal into HcsMetadata successful")

				assert.Equal(t, lastOriginalSequenceProcessed+1, hcsMetadata.LastResubmittedConfigSequence, "Expected lastResubmittedConfigSequence correct")
				assert.Equal(t, lastOriginalSequenceProcessed+1, hcsMetadata.LastOriginalSequenceProcessed, "Expected LastOriginalSequenceProcessed correct")
			case <-time.After(shortTimeout / 2):
				t.Fatalf("Expected one block being cut")
			}

			close(chain.haltChan)
			logger.Debug("haltChan closed")
			<-done

			assert.NoError(t, processErr, "Expected processMessages call to return without error")
		})

		t.Run("ResubmittedMsgStillBehind", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			lastOriginalSequenceProcessed := uint64(4)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:      mockblockcutter.NewReceiver(),
				Blocks:              make(chan *cb.Block),
				ChannelIDVal:        channelNameForTest(t),
				HeightVal:           lastCutBlockNumber,
				SharedConfigVal:     newMockOrderer(longTimeout, goodHcsTopicIDStr, publicKeys),
				SequenceVal:         uint64(2),
				ConfigSeqVal:        uint64(2),
				ProcessConfigMsgVal: newMockConfigEnvelope(),
				ChannelConfigVal:    newMockChannel(),
			}
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, lastOriginalSequenceProcessed, true, mockSupport, hcf)
			// to not panic on unexpected SequenceNumber
			chain.lastSequenceProcessed = 5
			chain.lastChunkFreeSequenceProcessed = 5
			setNextSequenceNumber(chain.topicSubscriptionHandle, lastOriginalSequenceProcessed+2)
			close(mockSupport.BlockCutterVal.Block)
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)
			done := make(chan struct{})

			go chain.processTimeToCutRequests()
			go func() {
				processErr = chain.processMessages()
				done <- struct{}{}
			}()

			// check that WaitReady is not blocked at beginning
			blockIngressMsg(t, true, chain.WaitReady)

			// emits a resubmitted config message with lagged config sequence
			msg := newConfigMessage(protoutil.MarshalOrPanic(newMockConfigEnvelope()), mockSupport.SequenceVal-1, lastOriginalSequenceProcessed+1)
			chunks, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err, "Expect Split returns no error")
			_, err = chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			assert.NoError(t, err, "Expect message sent successfully")
			select {
			case <-mockSupport.Blocks:
				t.Fatalf("Expected no block being cut")
			case <-time.After(shortTimeout / 2):
			}

			// should still block since resubmitted config message is still behind current config seq
			blockIngressMsg(t, true, chain.WaitReady)
			respSyncChan <- struct{}{} // unblock topicSubscriptionHandle so the next resubmission will go through

			select {
			case <-mockSupport.Blocks:
			case <-time.After(shortTimeout / 2):
				t.Fatalf("Expected block being cut")
			}
			respSyncChan <- struct{}{}

			// should no longer block
			blockIngressMsg(t, false, chain.WaitReady)

			close(chain.haltChan)
			logger.Debug("haltChan closed")
			<-done

			assert.NoError(t, processErr, "Expected processMessages call to return without errors")
		})

		t.Run("InvalidDiscard", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			lastOriginalSequenceProcessed := uint64(4)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:            mockblockcutter.NewReceiver(),
				Blocks:                    make(chan *cb.Block),
				ChannelIDVal:              channelNameForTest(t),
				HeightVal:                 lastCutBlockNumber,
				SharedConfigVal:           newMockOrderer(longTimeout, goodHcsTopicIDStr, publicKeys),
				SequenceVal:               uint64(1),
				ConfigSeqVal:              uint64(1),
				ProcessConfigUpdateMsgErr: fmt.Errorf("invalid config message"),
				ChannelConfigVal:          newMockChannel(),
			}
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, lastOriginalSequenceProcessed, false, mockSupport, hcf)
			close(mockSupport.BlockCutterVal.Block)
			respSyncChan := getRespSyncChan(chain.topicSubscriptionHandle)
			defer close(respSyncChan)
			done := make(chan struct{})

			go chain.processTimeToCutRequests()
			go func() {
				processErr = chain.processMessages()
				done <- struct{}{}
			}()

			// WaitReady should not be blocked
			blockIngressMsg(t, false, chain.WaitReady)

			// emits a config message with lagged configSeq, later it'll be invalidated
			msg := newConfigMessage(protoutil.MarshalOrPanic(newMockConfigEnvelope()), mockSupport.SequenceVal-1, uint64(0))
			chunks, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err, "Expect Split returns no error")
			_, err = chain.hcsClient.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			assert.NoError(t, err, "Expected message sent successfully")
			select {
			case <-mockSupport.Blocks:
				t.Fatalf("Expected no block being cut")
			case <-time.After(shortTimeout / 2):
			}
			respSyncChan <- struct{}{}

			close(chain.haltChan)
			logger.Debug("haltChan closed")
			<-done

			assert.NoError(t, processErr, "Expected processMessages call to return without errors")
		})

		t.Run("ValidResubmit", func(t *testing.T) {
			lastCutBlockNumber := uint64(3)
			lastOriginalSequenceProcessed := uint64(4)
			publicKeys := []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: testOperatorPublicKey}}
			mockSupport := &mockmultichannel.ConsenterSupport{
				BlockCutterVal:      mockblockcutter.NewReceiver(),
				Blocks:              make(chan *cb.Block),
				ChannelIDVal:        channelNameForTest(t),
				HeightVal:           lastCutBlockNumber,
				SharedConfigVal:     newMockOrderer(longTimeout, goodHcsTopicIDStr, publicKeys),
				SequenceVal:         uint64(1),
				ConfigSeqVal:        uint64(1),
				ProcessConfigMsgVal: newMockConfigEnvelope(),
				ChannelConfigVal:    newMockChannel(),
			}
			hcf := newDefaultMockHcsClientFactory()
			chain := newBareMinimumChain(t, lastCutBlockNumber, lastOriginalSequenceProcessed, false, mockSupport, hcf)
			// to not panic on unexpected SequenceNumber
			chain.lastChunkFreeSequenceProcessed = 5
			chain.lastSequenceProcessed = 5
			setNextSequenceNumber(chain.topicSubscriptionHandle, lastOriginalSequenceProcessed+2)
			close(mockSupport.BlockCutterVal.Block)
			close(getRespSyncChan(chain.topicSubscriptionHandle))
			done := make(chan struct{})

			// intercept the SubmitConsensusMessage call
			mockHcsClient := chain.hcsClient.(*mockhcs.HcsClient)
			oldStub := mockHcsClient.SubmitConsensusMessageStub
			mockHcsClient.SubmitConsensusMessageCalls(nil)
			mockHcsClient.SubmitConsensusMessageReturns(&hedera.TransactionID{}, nil)

			go chain.processTimeToCutRequests()
			go func() {
				processErr = chain.processMessages()
				done <- struct{}{}
			}()

			// check that WaitReady is not blocked at beginning
			blockIngressMsg(t, false, chain.WaitReady)

			// emits a config message with lagged sequence
			msg := newConfigMessage(protoutil.MarshalOrPanic(newMockConfigEnvelope()), mockSupport.SequenceVal-1, uint64(0))
			chunks, _, err := chain.appMsgProcessor.Split(protoutil.MarshalOrPanic(msg), time.Now())
			assert.Equal(t, 1, len(chunks), "Expect one chunk created from test message")
			assert.NoError(t, err, "Expect Split returns no error")
			_, err = oldStub(protoutil.MarshalOrPanic(chunks[0]), chain.topicID, nil)
			assert.NoError(t, err, "Expected message injected successfully")
			select {
			case <-mockSupport.Blocks:
				t.Fatalf("Expected no block being cut")
			case <-time.After(shortTimeout / 2):
			}

			assert.Equal(t, 1, mockHcsClient.SubmitConsensusMessageCallCount(), "Expected SubmitConsensusMessage called once")
			data, topicID, txID := mockHcsClient.SubmitConsensusMessageArgsForCall(0)

			// WaitReady should be blocked now,
			blockIngressMsg(t, true, chain.WaitReady)

			// now send the resubmitted config message
			_, err = oldStub(data, topicID, txID)
			assert.NoError(t, err, "Expected SubmitConsensusMessage returns without errors")

			select {
			case <-mockSupport.Blocks:
			case <-time.After(shortTimeout / 2):
				t.Fatalf("Expected block being cut")
			}

			// WaitReady is unblocked
			blockIngressMsg(t, false, chain.WaitReady)

			close(chain.haltChan)
			logger.Debug("haltChan closed")
			<-done

			assert.NoError(t, processErr, "Expected processMessages call to return without errors")
		})
	})
}

func TestHealthCheck(t *testing.T) {
	mockSupport := &mockmultichannel.ConsenterSupport{ChannelIDVal: channelNameForTest(t)}
	hcf := newDefaultMockHcsClientFactory()
	hcsClient, _ := hcf.GetHcsClient(nil, "", nil, nil)
	chain := &chainImpl{
		ConsenterSupport: mockSupport,
		hcsClient:        hcsClient,
		network: map[string]hedera.AccountID{
			"127.0.0.1:52011": {
				Shard:   0,
				Realm:   0,
				Account: 19988,
			},
			"127.0.0.2:52011": {
				Shard:   0,
				Realm:   0,
				Account: 19989,
			},
		},
	}
	mockHcsClient := hcsClient.(*mockhcs.HcsClient)

	t.Run("Proper", func(t *testing.T) {
		mockHcsClient.PingReturns(nil)
		assert.NoError(t, chain.HealthCheck(context.Background()), "Expected HealthCHeck returns no error")
	})

	t.Run("WithError", func(t *testing.T) {
		mockHcsClient.PingReturns(fmt.Errorf("test error message"))
		assert.Error(t, chain.HealthCheck(context.Background()), "Expected HealthCHeck returns error")
	})
}

func TestGetStateFromMetadata(t *testing.T) {
	lastConsensusTimestampPersisted := unixEpoch.Add(4 * time.Hour)
	lastOriginalSequenceProcessed := uint64(8)
	lastResubmittedConfigSequence := uint64(6)
	lastChunkFreeConsensusTimestamp := unixEpoch.Add(3 * time.Hour)
	lastChunkFreeSequenceProcessed := uint64(5)

	t.Run("NilMetadataExpectDetaultsReturned", func(t *testing.T) {
		lastConsensusTimestampPersisted, lastOriginalSequenceProcessed, lastResubmittedConfigSequence, lastChunkFreeConsensusTimestamp, lastChunkFreeSequenceProcessed := getStateFromMetadata(nil, "test-channeL")
		assert.Equal(t, unixEpoch, lastConsensusTimestampPersisted, "Expected lastConsensusTimestampPersisted to be unix epoch")
		assert.Equal(t, uint64(0), lastOriginalSequenceProcessed, "Expected lastOriginalSequenceProcessed to be 0")
		assert.Equal(t, uint64(0), lastResubmittedConfigSequence, "Expected lastResubmittedConfigSequence to be 0")
		assert.Equal(t, unixEpoch, lastChunkFreeConsensusTimestamp, "Expected lastChunkFreeConsensusTimestamp to be unix epoch")
		assert.Equal(t, uint64(0), lastChunkFreeSequenceProcessed, "Expected lastChunkFreeSequenceProcessed to be 0")
	})

	t.Run("ValidMetadataProper", func(t *testing.T) {
		metadataValue := protoutil.MarshalOrPanic(newHcsMetadata(
			timestampProtoOrPanic(lastConsensusTimestampPersisted),
			lastOriginalSequenceProcessed,
			lastResubmittedConfigSequence,
			timestampProtoOrPanic(lastChunkFreeConsensusTimestamp),
			lastChunkFreeSequenceProcessed,
		))
		returnedLastConsensusTimestampPersisted, returnedLastOriginalSequenceProcessed, returnedLastResubmittedConfigSequence, returnedLastchunkFreeConsensusTimestamp, returnedLastChunkFreeSequenceProcessed := getStateFromMetadata(metadataValue, "test-channel")
		assert.Equal(t, lastConsensusTimestampPersisted.UnixNano(), returnedLastConsensusTimestampPersisted.UnixNano(), "Expected returned lastConsensusTimestampPersisted match")
		assert.Equal(t, lastOriginalSequenceProcessed, returnedLastOriginalSequenceProcessed, "Expected returned lastOriginalSequenceProcessed match")
		assert.Equal(t, lastResubmittedConfigSequence, returnedLastResubmittedConfigSequence, "Expected returned lastOriginalSequenceProcessed match")
		assert.Equal(t, lastChunkFreeConsensusTimestamp.UnixNano(), returnedLastchunkFreeConsensusTimestamp.UnixNano(), "Expected returned lastchunkFreeConsensusTimestamp match")
		assert.Equal(t, lastChunkFreeSequenceProcessed, returnedLastChunkFreeSequenceProcessed, "Expected returned lastChunkFreeSequenceProcessed match")
	})

	t.Run("CorruptedMetadata", func(t *testing.T) {
		assert.Panics(t, func() { getStateFromMetadata(make([]byte, 4), "test-channel") }, "Expected getStateFromMetadata panic with corrupted metadata")
	})

	t.Run("NilLastConsensusTimestampPersisted", func(t *testing.T) {
		metadataValue := protoutil.MarshalOrPanic(newHcsMetadata(
			nil,
			lastOriginalSequenceProcessed,
			lastResubmittedConfigSequence,
			timestampProtoOrPanic(lastChunkFreeConsensusTimestamp),
			lastChunkFreeSequenceProcessed,
		))
		assert.Panics(t, func() { getStateFromMetadata(metadataValue, "test-channel") }, "Expected getStateFromMetadata panic with nil LastConsensusTimestampPersisted")
	})

	t.Run("NilLastchunkFreeConsensusTimestamp", func(t *testing.T) {
		metadataValue := protoutil.MarshalOrPanic(newHcsMetadata(
			timestampProtoOrPanic(lastConsensusTimestampPersisted),
			lastOriginalSequenceProcessed,
			lastResubmittedConfigSequence,
			nil,
			lastChunkFreeSequenceProcessed,
		))
		assert.Panics(t, func() { getStateFromMetadata(metadataValue, "test-channel") }, "Expected getStateFromMetadata panic with nil LastChunkFreeConsensusTimestamp")
	})
}

func TestParseConfig(t *testing.T) {
	mockHcsConfig := mockLocalConfig.Hcs
	mockHcsConfigMetadata := protoutil.MarshalOrPanic(&hb.HcsConfigMetadata{
		TopicId: goodHcsTopicIDStr,
		PublicKeys: []*hb.HcsConfigPublicKey{
			{
				Type: "ed25519",
				Key:  "302a300506032b657003210023452d9c2cc4e01deb670780f8e6d4e31badd1f5d2f5464971b490232a601c30",
			},
			{
				Type: "ed25519",
				Key:  "302a300506032b6570032100708e6eef139eaeba32f91c1a36230bd2d9c10b43a7a48308bc9e056107ac312a",
			},
		},
		ReassembleTimeout: "30s",
	})

	t.Run("WithValidConfig", func(t *testing.T) {
		topicID, publicKeys, reassembleTimeout, network, operatorID, privateKey, err := parseConfig(mockHcsConfigMetadata, &mockHcsConfig)

		assert.NoError(t, err, "Expected parseConfig returns no errors")
		assert.Equal(t, goodHcsTopicIDStr, topicID.String(), "Expect correct topicID")
		assert.Equal(t, 3, len(publicKeys), "Expected publicKeys have correct number of entries")
		assert.Equal(t, 30*time.Second, reassembleTimeout, "Expected reassembleTimeout to be 30s")
		assert.NotNil(t, network, "Expect non-nil chain.network")
		assert.Equal(t, len(mockHcsConfig.Nodes), len(network), "Expect chain.network has correct number of entries")
		assert.Equal(t, mockHcsConfig.Operator.Id, operatorID.String(), "Expect correct operator ID string")
		assert.Equal(t, mockHcsConfig.Operator.PrivateKey.Key, privateKey.String(), "Expect correct operator private key")
	})

	t.Run("WithValidPEMKey", func(t *testing.T) {
		localHcsConfig := mockHcsConfig
		rawKey, _ := hex.DecodeString(mockHcsConfig.Operator.PrivateKey.Key)
		block := &pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: rawKey,
		}
		localHcsConfig.Operator.PrivateKey.Key = string(pem.EncodeToMemory(block))
		topicID, publicKeys, reassembleTimeout, network, operatorID, privateKey, err := parseConfig(mockHcsConfigMetadata, &localHcsConfig)

		assert.NoError(t, err, "Expected parseConfig returns no errors")
		assert.Equal(t, goodHcsTopicIDStr, topicID.String(), "Expect correct topicID")
		assert.Equal(t, 3, len(publicKeys), "Expected publicKeys have correct number of entries")
		assert.Equal(t, 30*time.Second, reassembleTimeout, "Expected reassembleTimeout to be 30s")
		assert.NotNil(t, network, "Expect non-nil chain.network")
		assert.Equal(t, len(mockHcsConfig.Nodes), len(network), "Expect chain.network has correct number of entries")
		assert.Equal(t, mockHcsConfig.Operator.Id, operatorID.String(), "Expect correct operator ID string")
		assert.Equal(t, mockHcsConfig.Operator.PrivateKey.Key, privateKey.String(), "Expect correct operator private key")
	})

	t.Run("WithEmptyNodes", func(t *testing.T) {
		invalidMockHcsConfig := mockHcsConfig
		invalidMockHcsConfig.Nodes = make(map[string]string)
		_, _, _, _, _, _, err := parseConfig(mockHcsConfigMetadata, &invalidMockHcsConfig)
		assert.Error(t, err, "Expected parseConfig returns error when Nodes in HcsConfig is empty")
	})

	t.Run("WithInvalidAccountIDInNodes", func(t *testing.T) {
		invalidMockHcsConfig := mockHcsConfig
		invalidMockHcsConfig.Nodes = map[string]string{
			"127.0.0.1:50211": "0.0.3",
			"127.0.0.2:50211": "invalid account id",
		}
		_, _, _, _, _, _, err := parseConfig(mockHcsConfigMetadata, &invalidMockHcsConfig)
		assert.Error(t, err, "Expected parseConfig returns err when account ID in Nodes in invalid")

	})

	t.Run("WithInvalidOperatorID", func(t *testing.T) {
		invalidMockHcsConfig := mockHcsConfig
		invalidMockHcsConfig.Operator.Id = "invalid operator id"
		_, _, _, _, _, _, err := parseConfig(mockHcsConfigMetadata, &invalidMockHcsConfig)
		assert.Error(t, err, "Expected parseConfig returns error when operator ID is invalid")
	})

	// SDK regression
	// t.Run("WithInvalidPrivateKey", func(t *testing.T) {
	// 	invalidMockHcsConfig := mockHcsConfig
	// 	invalidMockHcsConfig.Operator.PrivateKey.Key = "invalid key string"
	// 	_, _, _, _, _, _, err := parseConfig(mockHcsConfigMetadata, &invalidMockHcsConfig)
	// 	assert.Error(t, err, "Expected parseConfig returns error when operator private key is invalid")
	// })

	t.Run("WithInvalidHCSTopicID", func(t *testing.T) {
		invalidHcsConfigMetadata := protoutil.MarshalOrPanic(&hb.HcsConfigMetadata{
			TopicId: "0.0.abcd",
			PublicKeys: []*hb.HcsConfigPublicKey{
				{
					Type: "ed25519",
					Key:  "302a300506032b657003210023452d9c2cc4e01deb670780f8e6d4e31badd1f5d2f5464971b490232a601c30",
				},
				{
					Type: "ed25519",
					Key:  "302a300506032b6570032100708e6eef139eaeba32f91c1a36230bd2d9c10b43a7a48308bc9e056107ac312a",
				},
			},
			ReassembleTimeout: "30s",
		})
		_, _, _, _, _, _, err := parseConfig(invalidHcsConfigMetadata, &mockHcsConfig)
		assert.Error(t, err, "Expected parseConfig returns error when hcs topic ID is invalid")
	})

	t.Run("WithInvalidPublicKey", func(t *testing.T) {
		invalidHcsConfigMetadata := protoutil.MarshalOrPanic(&hb.HcsConfigMetadata{
			TopicId:           goodHcsTopicIDStr,
			PublicKeys:        []*hb.HcsConfigPublicKey{{Type: "ed25519", Key: "invalid key"}},
			ReassembleTimeout: "30s",
		})
		_, _, _, _, _, _, err := parseConfig(invalidHcsConfigMetadata, &mockHcsConfig)
		assert.Error(t, err, "Expected parseConfig returns error when public key is invalid")
	})

	t.Run("WithUnsupportedPublicKeyType", func(t *testing.T) {
		invalidHcsConfigMetadata := protoutil.MarshalOrPanic(&hb.HcsConfigMetadata{
			TopicId: goodHcsTopicIDStr,
			PublicKeys: []*hb.HcsConfigPublicKey{
				{
					Type: "unknown",
					Key:  "302a300506032b6570032100708e6eef139eaeba32f91c1a36230bd2d9c10b43a7a48308bc9e056107ac312a",
				},
			},
			ReassembleTimeout: "30s",
		})
		_, _, _, _, _, _, err := parseConfig(invalidHcsConfigMetadata, &mockHcsConfig)
		assert.Error(t, err, "Expected parseConfig returns error when public key is invalid")
	})

	t.Run("WithInvalidDurationFormat", func(t *testing.T) {
		invalidHcsConfigMetadata := protoutil.MarshalOrPanic(&hb.HcsConfigMetadata{
			TopicId: goodHcsTopicIDStr,
			PublicKeys: []*hb.HcsConfigPublicKey{
				{
					Type: "ed25519",
					Key:  "302a300506032b657003210023452d9c2cc4e01deb670780f8e6d4e31badd1f5d2f5464971b490232a601c30",
				},
				{
					Type: "ed25519",
					Key:  "302a300506032b6570032100708e6eef139eaeba32f91c1a36230bd2d9c10b43a7a48308bc9e056107ac312a",
				},
			},
			ReassembleTimeout: "30",
		})
		_, _, _, _, _, _, err := parseConfig(invalidHcsConfigMetadata, &mockHcsConfig)
		assert.Error(t, err, "Expected parseConfig returns error when ReassembleTimeout invalid")
	})

	t.Run("WithZeroReassembleTimeout", func(t *testing.T) {
		invalidHcsConfigMetadata := protoutil.MarshalOrPanic(&hb.HcsConfigMetadata{
			TopicId: goodHcsTopicIDStr,
			PublicKeys: []*hb.HcsConfigPublicKey{
				{
					Type: "ed25519",
					Key:  "302a300506032b657003210023452d9c2cc4e01deb670780f8e6d4e31badd1f5d2f5464971b490232a601c30",
				},
				{
					Type: "ed25519",
					Key:  "302a300506032b6570032100708e6eef139eaeba32f91c1a36230bd2d9c10b43a7a48308bc9e056107ac312a",
				},
			},
			ReassembleTimeout: "0s",
		})
		_, _, _, _, _, _, err := parseConfig(invalidHcsConfigMetadata, &mockHcsConfig)
		assert.Error(t, err, "Expected parseConfig returns error when ReassembleTimeout is 0")
	})

	t.Run("WithCorruptedMetadata", func(t *testing.T) {
		_, _, _, _, _, _, err := parseConfig([]byte("corrupted metadata"), &mockHcsConfig)
		assert.Error(t, err, "Expected parseConfig returns error when configuration metadata is corrupted")
	})
}

func TestParseEd25519PrivateKey(t *testing.T) {
	t.Run("WithValidHexEncodedString", func(t *testing.T) {
		skey, err := parseEd25519PrivateKey(testOperatorPrivateKey)
		assert.NoError(t, err, "Expected parsePrivateKey returns no error")
		assert.Equal(t, testOperatorPrivateKey, skey.String(), "Expected the parsed key matches the input")
	})

	t.Run("WithValidPEMEncodedKey", func(t *testing.T) {
		rawKey, _ := hex.DecodeString(testOperatorPrivateKey)
		block := &pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: rawKey,
		}
		skey, err := parseEd25519PrivateKey(string(pem.EncodeToMemory(block)))
		assert.NoError(t, err, "Expected parsePrivateKey returns no error")
		assert.Equal(t, testOperatorPrivateKey, skey.String(), "Expected the parsed key matches the input")
	})

	// SDK regression
	// t.Run("WithInvalidKey", func(t *testing.T) {
	// 	invalidKey := testOperatorPrivateKey + "invalid"
	// 	_, err := parseEd25519PrivateKey(invalidKey)
	// 	assert.Errorf(t, err, "Expected parsePrivateKey returns error")
	// })

	t.Run("WithInvalidPEMType", func(t *testing.T) {
		rawKey, _ := hex.DecodeString(testOperatorPrivateKey)
		block := &pem.Block{
			Type:  "BAD KEY TYPE",
			Bytes: rawKey,
		}
		_, err := parseEd25519PrivateKey(string(pem.EncodeToMemory(block)))
		assert.Errorf(t, err, "Expected parsePrivateKey returns error")
	})

	t.Run("WithInvalidPEMKeyBytes", func(t *testing.T) {
		block := &pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: []byte("invalid private key bytes"),
		}
		_, err := parseEd25519PrivateKey(string(pem.EncodeToMemory(block)))
		assert.Errorf(t, err, "Expected parsePrivateKey returns error")
	})
}

func TestNewConfigMessage(t *testing.T) {
	data := []byte("test message")
	configSeq := uint64(3)
	originalSeq := uint64(8)
	msg := newConfigMessage(data, configSeq, originalSeq)
	assert.IsType(t, &hb.HcsMessage_Regular{}, msg.Type, "Expected message type to be HcsMessage_Regular")
	regular := msg.Type.(*hb.HcsMessage_Regular)
	assert.IsType(t, &hb.HcsMessageRegular{}, regular.Regular, "Expected message type to be HcsMessageRegular")
	config := regular.Regular
	assert.Equal(t, data, config.Payload, "Expected payload to match")
	assert.Equal(t, configSeq, config.ConfigSeq, "Expected configSeq to match")
	assert.Equal(t, hb.HcsMessageRegular_CONFIG, config.Class, "Expected Class to be CONFIG")
	assert.Equal(t, originalSeq, config.OriginalSeq, "Expected OriginalSeq to match")
}

func TestNewNormalMessage(t *testing.T) {
	data := []byte("test message")
	configSeq := uint64(3)
	originalSeq := uint64(8)
	msg := newNormalMessage(data, configSeq, originalSeq)
	assert.IsType(t, &hb.HcsMessage_Regular{}, msg.Type, "Expected message type to be HcsMessage_Regular")
	regular := msg.Type.(*hb.HcsMessage_Regular)
	assert.IsType(t, &hb.HcsMessageRegular{}, regular.Regular, "Expected message type to be HcsMessageRegular")
	config := regular.Regular
	assert.Equal(t, data, config.Payload, "Expected payload to match")
	assert.Equal(t, configSeq, config.ConfigSeq, "Expected configSeq to match")
	assert.Equal(t, hb.HcsMessageRegular_NORMAL, config.Class, "Expected Class to be NORMAL")
	assert.Equal(t, originalSeq, config.OriginalSeq, "Expected OriginalSeq to match")
}

func TestNewTimeToCutMessageWithBlockNumber(t *testing.T) {
	blockNumber := uint64(9)
	msg := newTimeToCutMessageWithBlockNumber(blockNumber)
	assert.IsType(t, &hb.HcsMessage_TimeToCut{}, msg.Type, "Expected message type to be HcsMessage_TimeToCut")
	regular := msg.Type.(*hb.HcsMessage_TimeToCut)
	assert.IsType(t, &hb.HcsMessageTimeToCut{}, regular.TimeToCut, "Expected message type to be HcsMessageTimeToCut")
	ttc := regular.TimeToCut
	assert.IsType(t, &hb.HcsMessageTimeToCut_BlockNumber{}, ttc.Request, "Expected message type to be HcsMessageTimeToCut_BlockNumber")
	assert.Equal(t, blockNumber, ttc.GetBlockNumber(), "Expected blockNumber to match")
}

func TestNewTimeToCutMessageWithMessageHash(t *testing.T) {
	messageHash := []byte("dummy hash")
	msg := newTimeToCutMessageWithMessageHash(messageHash)
	assert.IsType(t, &hb.HcsMessage_TimeToCut{}, msg.Type, "Expected message type to be HcsMessage_TimeToCut")
	regular := msg.Type.(*hb.HcsMessage_TimeToCut)
	assert.IsType(t, &hb.HcsMessageTimeToCut{}, regular.TimeToCut, "Expected message type to be HcsMessageTimeToCut")
	ttc := regular.TimeToCut
	assert.IsType(t, &hb.HcsMessageTimeToCut_MessageHash{}, ttc.Request, "Expected message type to be HcsMessageTimeToCut_BlockNumber")
	assert.Equal(t, messageHash, ttc.GetMessageHash(), "Expected messageHash to match")
}

func TestNewOrdererStartedMessage(t *testing.T) {
	identity := []byte("test orderer identity")
	msg := newOrdererStartedMessage(identity)
	assert.IsType(t, &hb.HcsMessage_OrdererStarted{}, msg.Type, "Expected message type to be HcsMessage_OrdererStarted")
	ordererStartedMsg := msg.Type.(*hb.HcsMessage_OrdererStarted)
	assert.IsType(t, &hb.HcsMessageOrdererStarted{}, ordererStartedMsg.OrdererStarted, "Expected message type to be HcsMessageOrdererStarted")
	assert.Equal(t, identity, ordererStartedMsg.OrdererStarted.OrdererIdentity, "Expected identity to match")
}

func TestNewHcsMetadata(t *testing.T) {
	lastConsensusTimestampPersisted := ptypes.TimestampNow()
	lastOriginalSequenceProcessed := uint64(12)
	lastResubmittedConfigSequence := uint64(25)
	lastChunkFreeConsensusTimestamp := lastConsensusTimestampPersisted
	lastChunkFreeSequenceProcessed := uint64(3)
	metadata := newHcsMetadata(
		lastConsensusTimestampPersisted,
		lastOriginalSequenceProcessed,
		lastResubmittedConfigSequence,
		lastChunkFreeConsensusTimestamp,
		lastChunkFreeSequenceProcessed,
	)
	assert.Equal(t, lastConsensusTimestampPersisted, metadata.LastConsensusTimestampPersisted, "Exepcted correct LastConsensusTimestampPersisted")
	assert.Equal(t, lastOriginalSequenceProcessed, metadata.LastOriginalSequenceProcessed, "Expected correct LastOriginalSequenceProcessed")
	assert.Equal(t, lastResubmittedConfigSequence, metadata.LastResubmittedConfigSequence, "Expected correct LastResubmittedConfigSequence")
	assert.Equal(t, lastChunkFreeConsensusTimestamp, metadata.LastChunkFreeConsensusTimestampPersisted, "Expected correct LastChunkFreeConsensusTimestampPersisted")
	assert.Equal(t, lastChunkFreeSequenceProcessed, metadata.LastChunkFreeSequenceProcessed, "Expected correct LastChunkFreeConsensusTimestampPersisted")
}

func TestTimestampProtoOrPanic(t *testing.T) {
	t.Run("Proper", func(t *testing.T) {
		var ts *timestamp.Timestamp
		assert.NotPanics(t, func() { ts = timestampProtoOrPanic(unixEpoch) }, "Expected no panic with valid time")
		assert.Equal(t, unixEpoch.Second(), int(ts.Seconds), "Expected seconds equal")
		assert.Equal(t, unixEpoch.Nanosecond(), int(ts.Nanos), "Expected nanoseconds equal")
	})

	t.Run("NilTime", func(t *testing.T) {
		invalidTime := time.Time{}.Add(-100 * time.Hour)
		assert.Panics(t, func() { timestampProtoOrPanic(invalidTime) }, "Expected panic with nil passed in")
	})
}

// Test helper functions

type messageWithMetadata struct {
	message            []byte
	consensusTimestamp *time.Time
	sequenceNumber     *uint64
}

type mockHcsTransport struct {
	l        sync.Mutex
	channels map[hedera.TopicID]chan messageWithMetadata
	states   map[hedera.TopicID]*topicState
}

func (t *mockHcsTransport) getTransportW(topicID *hedera.TopicID) chan<- messageWithMetadata {
	transport, _ := t.getTransport(topicID)
	return transport
}

func (t *mockHcsTransport) getTransportRWithTopicState(topicID *hedera.TopicID) (<-chan messageWithMetadata, *topicState) {
	return t.getTransport(topicID)
}

func (t *mockHcsTransport) getTransport(topicID *hedera.TopicID) (chan messageWithMetadata, *topicState) {
	t.l.Lock()
	defer t.l.Unlock()

	ch, ok := t.channels[*topicID]
	if !ok {
		ch = make(chan messageWithMetadata)
		t.channels[*topicID] = ch
	}
	state, ok := t.states[*topicID]
	if !ok {
		state = &topicState{
			consensusTimestamp: time.Now(),
			sequenceNumber:     uint64(1),
		}
		t.states[*topicID] = state
	}
	return ch, state
}

func newMockHcsTransport() *mockHcsTransport {
	return &mockHcsTransport{
		channels: make(map[hedera.TopicID]chan messageWithMetadata),
		states:   make(map[hedera.TopicID]*topicState),
	}
}

type topicState struct {
	consensusTimestamp time.Time
	sequenceNumber     uint64
}

type hcsClientFactoryWithRecord struct {
	mockhcs.HcsClientFactory
	transport    *mockHcsTransport
	initData     map[hedera.TopicID]topicState
	returnValues map[string][]interface{}
	l            sync.Mutex
}

func (f *hcsClientFactoryWithRecord) withInitTopicState(topicID hedera.TopicID, initState topicState) *hcsClientFactoryWithRecord {
	f.transport.states[topicID] = &initState
	return f
}

func (f *hcsClientFactoryWithRecord) InjectMessageWithMetadata(message []byte, consensusTimestamp *time.Time, sequenceNumber *uint64, topicID *hedera.TopicID) error {
	if message == nil {
		return errors.Errorf("message is nil")
	}
	ch := f.transport.getTransportW(topicID)
	ch <- messageWithMetadata{
		message:            message,
		consensusTimestamp: consensusTimestamp,
		sequenceNumber:     sequenceNumber,
	}
	return nil
}

func (f *hcsClientFactoryWithRecord) InjectMessage(message []byte, topicID *hedera.TopicID) error {
	return f.InjectMessageWithMetadata(message, nil, nil, topicID)
}

func (f *hcsClientFactoryWithRecord) GetReturnValues() map[string][]interface{} {
	f.l.Lock()
	defer f.l.Unlock()

	dup := map[string][]interface{}{}
	for key, value := range f.returnValues {
		dup[key] = value
	}
	return dup
}

func newDefaultMockHcsClientFactory() *hcsClientFactoryWithRecord {
	return newMockHcsClientFactory(nil, false)
}

func newMockHcsClientFactoryWithReadonlyHcsClient() *hcsClientFactoryWithRecord {
	return newMockHcsClientFactory(nil, true)
}

func newMockHcsClientFactory(
	getHcsClient func(map[string]hedera.AccountID, string, *hedera.AccountID, *hedera.PrivateKey) (factory.HcsClient, error),
	readonly bool,
) *hcsClientFactoryWithRecord {
	mock := &hcsClientFactoryWithRecord{transport: newMockHcsTransport(), returnValues: make(map[string][]interface{})}

	recordReturnValue := func(key string, returnValues []interface{}) {
		mock.l.Lock()
		if mock.returnValues == nil {
			mock.returnValues = map[string][]interface{}{}
		}
		if mock.returnValues[key] == nil {
			mock.returnValues[key] = []interface{}{}
		}
		mock.returnValues[key] = append(mock.returnValues[key], returnValues)
		mock.l.Unlock()
	}

	defaultGetHcsClient := func(
		network map[string]hedera.AccountID,
		mirrorEndpoint string,
		account *hedera.AccountID,
		key *hedera.PrivateKey,
	) (factory.HcsClient, error) {
		c := mockhcs.HcsClient{}
		c.CloseReturns(nil)
		if !readonly {
			c.SubmitConsensusMessageCalls(func(
				message []byte,
				topicID *hedera.TopicID,
				txID *hedera.TransactionID,
			) (*hedera.TransactionID, error) {
				if message == nil {
					return nil, errors.Errorf("message is nil")
				}
				ch := mock.transport.getTransportW(topicID)
				ch <- messageWithMetadata{message: message}
				if txID == nil {
					txID = &hedera.TransactionID{}
				}
				return txID, nil
			})
		} else {
			c.SubmitConsensusMessageReturns(&hedera.TransactionID{}, nil)
		}

		c.SubscribeTopicCalls(func(
			topicID *hedera.TopicID,
			start *time.Time,
			end *time.Time,
		) (factory.MirrorSubscriptionHandle, error) {
			transport, state := mock.transport.getTransportRWithTopicState(topicID)
			handle := newMockMirrorSubscriptionHandle(transport, state)
			handle.start()
			return handle, nil
		})

		return &c, nil
	}

	innerGetHcsClient := defaultGetHcsClient
	if getHcsClient != nil {
		innerGetHcsClient = getHcsClient
	}

	getHcsClientWithRecord := func(
		network map[string]hedera.AccountID,
		mirrorEndpoint string,
		account *hedera.AccountID,
		key *hedera.PrivateKey,
	) (client factory.HcsClient, err error) {
		defer func() {
			recordReturnValue(getHcsClientFuncName, []interface{}{client, err})
		}()
		client, err = innerGetHcsClient(network, mirrorEndpoint, account, key)
		return client, err
	}

	mock.GetHcsClientCalls(getHcsClientWithRecord)
	return mock
}

type mockMirrorSubscriptionHandle struct {
	transport    <-chan messageWithMetadata
	msgChan      chan *hedera.TopicMessage
	errChan      chan status.Status
	errChanIn    chan status.Status
	done         chan struct{}
	l            sync.Mutex
	state        *topicState
	respSyncChan chan struct{}
}

func (h *mockMirrorSubscriptionHandle) start() {
	go func() {
		state := h.state
	LOOP:
		for {
			select {
			case msgWithMeta, ok := <-h.transport:
				if !ok {
					h.errChan <- *status.New(codes.Unknown, "unknown error")
					return
				}
				// build consensus response
				h.l.Lock()
				if msgWithMeta.consensusTimestamp != nil {
					state.consensusTimestamp = *msgWithMeta.consensusTimestamp
				}
				if msgWithMeta.sequenceNumber != nil {
					state.sequenceNumber = *msgWithMeta.sequenceNumber
				}
				msg := hedera.TopicMessage{
					ConsensusTimestamp: state.consensusTimestamp,
					Contents:           msgWithMeta.message,
					RunningHash:        nil,
					SequenceNumber:     state.sequenceNumber,
				}
				h.l.Unlock()
				h.msgChan <- &msg

				h.l.Lock()
				state.consensusTimestamp = state.consensusTimestamp.Add(time.Nanosecond)
				state.sequenceNumber++
				h.l.Unlock()
				<-h.respSyncChan
			case <-h.done:
				//h.errChan <- fmt.Errorf("subscription is cancelled by caller")
				close(h.msgChan)
				close(h.errChan)
				close(h.errChanIn)
				return
			case err := <-h.errChanIn:
				h.errChan <- err
				close(h.errChanIn)
				break LOOP
			}
		}
		<-h.done
		close(h.msgChan)
		close(h.errChan)
	}()
}

func (h *mockMirrorSubscriptionHandle) setNextSequenceNumber(sequenceNumber uint64) {
	h.l.Lock()
	defer h.l.Unlock()
	h.state.sequenceNumber = sequenceNumber
}

func (h *mockMirrorSubscriptionHandle) getNextSequenceNumber() uint64 {
	h.l.Lock()
	defer h.l.Unlock()
	return h.state.sequenceNumber
}

func (h *mockMirrorSubscriptionHandle) setNextConsensusTimestamp(timestamp time.Time) {
	h.l.Lock()
	defer h.l.Unlock()
	h.state.consensusTimestamp = timestamp
}

func (h *mockMirrorSubscriptionHandle) getNextConsensusTimestamp() time.Time {
	h.l.Lock()
	defer h.l.Unlock()
	return h.state.consensusTimestamp
}

func (h *mockMirrorSubscriptionHandle) sendError(err *status.Status) {
	h.errChanIn <- *err
}

func (h *mockMirrorSubscriptionHandle) Unsubscribe() {
	select {
	case <-h.done:
	default:
		close(h.done)
	}
}

func (h *mockMirrorSubscriptionHandle) Messages() <-chan *hedera.TopicMessage {
	return h.msgChan
}

func (h *mockMirrorSubscriptionHandle) Errors() <-chan status.Status {
	return h.errChan
}

func newMockMirrorSubscriptionHandle(transport <-chan messageWithMetadata, state *topicState) *mockMirrorSubscriptionHandle {
	return &mockMirrorSubscriptionHandle{
		transport:    transport,
		msgChan:      make(chan *hedera.TopicMessage),
		errChan:      make(chan status.Status),
		errChanIn:    make(chan status.Status),
		done:         make(chan struct{}),
		respSyncChan: make(chan struct{}),
		state:        state,
	}
}

func getNextSequenceNumber(handle factory.MirrorSubscriptionHandle) uint64 {
	mockHandle := handle.(*mockMirrorSubscriptionHandle)
	return mockHandle.getNextSequenceNumber()
}

func setNextSequenceNumber(handle factory.MirrorSubscriptionHandle, sequence uint64) {
	mockHandle := handle.(*mockMirrorSubscriptionHandle)
	mockHandle.setNextSequenceNumber(sequence)
}

func getNextConsensusTimestamp(handle factory.MirrorSubscriptionHandle) time.Time {
	mockHandle := handle.(*mockMirrorSubscriptionHandle)
	return mockHandle.getNextConsensusTimestamp()
}

func setNextConsensusTimestamp(handle factory.MirrorSubscriptionHandle, ts time.Time) {
	mockHandle := handle.(*mockMirrorSubscriptionHandle)
	mockHandle.setNextConsensusTimestamp(ts)
}

func getRespSyncChan(handle factory.MirrorSubscriptionHandle) chan struct{} {
	mockHandle := handle.(*mockMirrorSubscriptionHandle)
	return mockHandle.respSyncChan
}

func sendError(handle factory.MirrorSubscriptionHandle, err *status.Status) {
	mockHandle := handle.(*mockMirrorSubscriptionHandle)
	mockHandle.sendError(err)
}

func newMockEnvelope(content string) *cb.Envelope {
	return &cb.Envelope{Payload: protoutil.MarshalOrPanic(&cb.Payload{
		Header: &cb.Header{ChannelHeader: protoutil.MarshalOrPanic(&cb.ChannelHeader{ChannelId: "foo"})},
		Data:   []byte(content),
	})}
}

func newMockEnvelopeWithRawData(content []byte) *cb.Envelope {
	return &cb.Envelope{Payload: protoutil.MarshalOrPanic(&cb.Payload{
		Header: &cb.Header{ChannelHeader: protoutil.MarshalOrPanic(&cb.ChannelHeader{ChannelId: "foo"})},
		Data:   content,
	})}
}

func newMockConfigEnvelope() *cb.Envelope {
	return &cb.Envelope{Payload: protoutil.MarshalOrPanic(&cb.Payload{
		Header: &cb.Header{ChannelHeader: protoutil.MarshalOrPanic(
			&cb.ChannelHeader{Type: int32(cb.HeaderType_CONFIG), ChannelId: "foo"})},
		Data: protoutil.MarshalOrPanic(&cb.ConfigEnvelope{}),
	})}
}

func extractConsensusTimestamp(block *cb.Block) *timestamp.Timestamp {
	omd, _ := protoutil.GetMetadataFromBlock(block, cb.BlockMetadataIndex_ORDERER)
	metadata := &hb.HcsMetadata{}
	_ = proto.Unmarshal(omd.GetValue(), metadata)
	return metadata.LastConsensusTimestampPersisted
}

func extractLastChunkFreeConsensusTimestamp(block *cb.Block) *timestamp.Timestamp {
	omd, _ := protoutil.GetMetadataFromBlock(block, cb.BlockMetadataIndex_ORDERER)
	metadata := &hb.HcsMetadata{}
	_ = proto.Unmarshal(omd.GetValue(), metadata)
	return metadata.GetLastChunkFreeConsensusTimestampPersisted()
}

func extractLastChunkFreeSequence(block *cb.Block) uint64 {
	omd, _ := protoutil.GetMetadataFromBlock(block, cb.BlockMetadataIndex_ORDERER)
	metadata := &hb.HcsMetadata{}
	_ = proto.Unmarshal(omd.GetValue(), metadata)
	return metadata.GetLastChunkFreeSequenceProcessed()
}
