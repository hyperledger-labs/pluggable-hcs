/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"context"
	"crypto/md5"
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/hashgraph/hedera-sdk-go"
	"github.com/hyperledger/fabric-protos-go/common"
	cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.com/hyperledger/fabric/orderer/consensus"
	"github.com/hyperledger/fabric/orderer/consensus/hcs/factory"
	hb "github.com/hyperledger/fabric/orderer/consensus/hcs/protodef"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ed25519"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	unixEpoch               = time.Unix(0, 0)
	defaultHcsClientFactory = &hcsClientFactoryImpl{}
)

const (
	maxConsensusMessageSize    = 960 // the max message size HCS supports is 1024, including header
	subscriptionRetryBaseDelay = 100 * time.Millisecond
	subscriptionRetryMax       = 8
)

func getStateFromMetadata(metadataValue []byte, channelID string) (time.Time, uint64, uint64, time.Time, uint64) {
	if metadataValue != nil {
		hcsMetadata := &hb.HcsMetadata{}
		if err := proto.Unmarshal(metadataValue, hcsMetadata); err != nil {
			logger.Panicf("[channel: %s] Ledger may be corrupted: "+
				"cannot unmarshal orderer metadata in most recent block", channelID)
		}
		consensusTimestamp, err := ptypes.Timestamp(hcsMetadata.GetLastConsensusTimestampPersisted())
		if err != nil {
			logger.Panicf("[channel: %s] Ledger may be corrupted: "+
				"invalid last consensus timestamp in most recent block, %v", channelID, err)
		}
		lastChunkFreeTimestamp, err := ptypes.Timestamp(hcsMetadata.LastChunkFreeConsensusTimestampPersisted)
		if err != nil {
			logger.Panicf("[channel: %s] Ledger may be corrupted: "+
				"invalid last chunk free consensus timestamp in most recent block, %v", channelID, err)
		}
		return consensusTimestamp,
			hcsMetadata.GetLastOriginalSequenceProcessed(),
			hcsMetadata.GetLastResubmittedConfigSequence(),
			lastChunkFreeTimestamp,
			hcsMetadata.GetLastChunkFreeSequenceProcessed()
	}

	// defaults
	return unixEpoch, 0, 0, unixEpoch, 0
}

func newChain(
	consenter commonConsenter,
	support consensus.ConsenterSupport,
	healthChecker healthChecker,
	hcf factory.HcsClientFactory,
	lastConsensusTimestampPersisted time.Time,
	lastOriginalSequenceProcessed uint64,
	lastResubmittedConfigSequence uint64,
	lastChunkFreeConsensusTimestamp time.Time,
	lastChunkFreeSequenceProcessed uint64,
) (*chainImpl, error) {
	lastCutBlockNumber := support.Height() - 1
	logger.Infof("[channel: %s] starting chain with last persisted consensus timestamp %d and "+
		"last recorded block [%d]", support.ChannelID(), lastConsensusTimestampPersisted.UnixNano(), lastCutBlockNumber)

	localHcsConfig := consenter.sharedHcsConfig()
	topicID, publicKeys, reassembleTimeout, network, operatorID, operatorPrivateKey, err := parseConfig(support.SharedConfig().ConsensusMetadata(), localHcsConfig)
	if err != nil {
		logger.Errorf("[channel: %s] err parsing config = %v", support.ChannelID(), err)
		return nil, err
	}

	doneReprocessingMsgInFlight := make(chan struct{})
	// If there are no chunks pending reassembly and one of the following cases is true, we should unblock ingress messages:
	// - lastResubmittedConfigOffset == 0, where we've never resubmitted any config messages
	// - lastResubmittedConfigOffset == lastOriginalOffsetProcessed, where the latest config message we resubmitted
	//   has been processed already
	// - lastResubmittedConfigOffset < lastOriginalOffsetProcessed, where we've processed one or more resubmitted
	//   normal messages after the latest resubmitted config message. (we advance `lastResubmittedConfigOffset` for
	//   config messages, but not normal messages)
	if lastConsensusTimestampPersisted.Equal(lastChunkFreeConsensusTimestamp) &&
		(lastResubmittedConfigSequence == 0 || lastResubmittedConfigSequence <= lastOriginalSequenceProcessed) {
		// If we've already caught up with the reprocessing resubmitted messages, close the channel to unblock broadcast
		close(doneReprocessingMsgInFlight)
	}

	appID := md5.Sum(consenter.identity())

	consenter.Metrics().NumberNodes.With("channel", support.ChannelID()).Set(float64(len(support.ChannelConfig().OrdererAddresses())))
	consenter.Metrics().CommittedBlockNumber.With("channel", support.ChannelID()).Set(float64(lastCutBlockNumber))
	consenter.Metrics().LastConsensusTimestampPersisted.With("channel", support.ChannelID()).Set(float64(lastConsensusTimestampPersisted.UnixNano()))

	errorChan := make(chan struct{})
	close(errorChan)

	chain := &chainImpl{
		consenter:                       consenter,
		ConsenterSupport:                support,
		hcf:                             hcf,
		lastConsensusTimestampPersisted: lastConsensusTimestampPersisted,
		lastConsensusTimestamp:          lastChunkFreeConsensusTimestamp,
		lastOriginalSequenceProcessed:   lastOriginalSequenceProcessed,
		lastResubmittedConfigSequence:   lastResubmittedConfigSequence,
		lastChunkFreeConsensusTimestamp: lastChunkFreeConsensusTimestamp,
		lastChunkFreeSequenceProcessed:  lastChunkFreeSequenceProcessed,
		lastSequenceProcessed:           lastChunkFreeSequenceProcessed,
		lastCutBlockNumber:              lastCutBlockNumber,
		network:                         network,
		operatorID:                      operatorID,
		operatorPrivateKey:              operatorPrivateKey,
		operatorPublicKeyBytes:          operatorPrivateKey.PublicKey().Bytes(),
		publicKeys:                      publicKeys,
		topicID:                         &topicID,
		errorChan:                       errorChan,
		haltChan:                        make(chan struct{}),
		startChan:                       make(chan struct{}),
		doneReprocessingMsgInFlight:     doneReprocessingMsgInFlight,
		timeToCutRequestChan:            make(chan timeToCutRequest),
		messageHashes:                   make(map[string]struct{}),
		nonceReader:                     crand.Reader,
		appID:                           appID[:],
	}

	if chain.appMsgProcessor, err = newAppMsgProcessor(*chain.operatorID, chain.appID, maxConsensusMessageSize, reassembleTimeout, chain); err != nil {
		logger.Errorf("[channel: %s] failed to create appMsgProcessor with chunk size %d bytes", chain.ChannelID(), maxConsensusMessageSize)
		return nil, fmt.Errorf("failed to create appMsgProcessor with chunk size %d bytes - %v", maxConsensusMessageSize, err)
	}

	healthChecker.RegisterChecker(topicID.String(), chain)
	return chain, nil
}

type chainImpl struct {
	consenter commonConsenter
	consensus.ConsenterSupport

	hcf factory.HcsClientFactory

	lastConsensusTimestampPersisted time.Time
	// track the consensus timestamp of the last received MirrorConsensusTopicResponse
	lastConsensusTimestamp time.Time
	// track the consensus timestamp of the last message in either the blockcutter or the last cut block (when blockcutter's queue is empty)
	lastOrdereredConsensusTimestamp time.Time
	lastOriginalSequenceProcessed   uint64
	lastResubmittedConfigSequence   uint64
	// track the consensus timestamp of the last block at the time it's cut, there is no pending chunks
	lastChunkFreeConsensusTimestamp time.Time
	// similar as above, while tracking the sequence number
	lastChunkFreeSequenceProcessed uint64
	// track the sequence number of the last received MirrorConsensusTopicResponse
	lastSequenceProcessed uint64
	lastCutBlockNumber    uint64

	// mutex used when changing the doneReprocessingMsgInFlight
	doneReprocessingMutex sync.Mutex
	// notification that there are in-flight messages need to wait for
	doneReprocessingMsgInFlight chan struct{}

	// when the topic consumer errors, close the channel. Otherwise, make this
	// an open, unbuffered channel
	errorChan   chan struct{}
	errorCMutex sync.RWMutex
	// when a Halt() request comes, close the channel.
	haltChan               chan struct{}
	doneProcessingMessages chan struct{}
	startChan              chan struct{}

	timeToCutRequestChan chan timeToCutRequest
	messageHashes        map[string]struct{}
	senderWaitGroup      sync.WaitGroup

	network                 map[string]hedera.AccountID
	operatorID              *hedera.AccountID
	operatorPrivateKey      *hedera.Ed25519PrivateKey
	operatorPublicKeyBytes  []byte
	publicKeys              map[string]*hedera.Ed25519PublicKey
	topicID                 *hedera.ConsensusTopicID
	topicProducer           factory.ConsensusClient
	topicConsumer           factory.MirrorClient
	topicSubscriptionHandle factory.MirrorSubscriptionHandle
	subscriptionRetryTimer  *time.Timer

	appMsgProcessor appMsgProcessor
	appID           []byte

	nonceReader io.Reader
}

type timeToCutRequest struct {
	start           bool   // start or stop the receiving side timer
	nextBlockNumber uint64 // nextBlockNumber, from receiving side
	messageHash     []byte // hash of the message, from sending side, when set, start and nextBlockNumber are ignored
}

func (chain *chainImpl) Order(env *common.Envelope, configSeq uint64) error {
	logger.Debugf("[channel: %s] begin processing of a new normal tx with config sequence %d",
		chain.ChannelID(), configSeq)
	return chain.order(env, configSeq, 0)
}

func (chain *chainImpl) Configure(config *common.Envelope, configSeq uint64) error {
	logger.Debugf("[channel: %s] begin processing of a new config tx with config sequence %d",
		chain.ChannelID(), configSeq)
	return chain.configure(config, configSeq, 0)
}

func (chain *chainImpl) WaitReady() error {
	select {
	case <-chain.startChan:
		select {
		case <-chain.haltChan: // The chain has been halted, stop here
			return fmt.Errorf("[channel: %s] consenter for this channel has been halted", chain.ChannelID())
		case <-chain.doneReprocessing(): // Block waiting for all re-submitted messages to be reprocessed
			select {
			// re-check haltChan since select is random. if the chain is halted, although it's done reprocessing,
			// error should be returned
			case <-chain.haltChan:
				return fmt.Errorf("[channel: %s] consenter for this channel has been halted", chain.ChannelID())
			default:
				return nil
			}
		}
	default:
		return fmt.Errorf("consenter has not completed bootstraping; try again later")
	}
}

func (chain *chainImpl) Start() {
	go startThread(chain)
}

func (chain *chainImpl) Halt() {
	select {
	case <-chain.startChan:
		// chain finished starting, so we can halt it
		select {
		case <-chain.haltChan:
			// This construct is useful because it allows Halt() to be called
			// multiple times (by a single thread) w/o panicking. Recall that a
			// receive from a closed channel returns (the zero value) immediately.
			logger.Warningf("[channel: %s] Halting of chain requested again", chain.ChannelID())
		default:
			logger.Criticalf("[channel: %s] Halting of chain requested", chain.ChannelID())
			// stat shutdown of chain
			close(chain.haltChan)
			// wait for processing of messages to blocks to finish shutting down
			<-chain.doneProcessingMessages
			logger.Debugf("[channel: %s] Closed the haltChan", chain.ChannelID())
		}
	default:
		logger.Warningf("[channel: %s] Waiting for chain to finish starting before halting", chain.ChannelID())
		<-chain.startChan
		chain.Halt()
	}
}

func (chain *chainImpl) Errored() <-chan struct{} {
	chain.errorCMutex.RLock()
	defer chain.errorCMutex.RUnlock()
	return chain.errorChan
}

// signer interface
func (chain *chainImpl) Sign(message []byte) ([]byte, []byte, error) {
	if message == nil || len(message) == 0 {
		return nil, nil, fmt.Errorf("invalid data")
	}
	return chain.operatorPublicKeyBytes, chain.operatorPrivateKey.Sign(message), nil
}

func (chain *chainImpl) Verify(message, signerPublicKey, signature []byte) bool {
	if message == nil || signerPublicKey == nil || signature == nil {
		return false
	}
	if _, ok := chain.publicKeys[string(signerPublicKey)]; !ok {
		return false
	}
	return ed25519.Verify(signerPublicKey, message, signature)
}

func (chain *chainImpl) processTimeToCutRequests() {
	createStoppedTimer := func() *time.Timer {
		t := time.NewTimer(time.Second)
		if !t.Stop() {
			<-t.C
		}
		return t
	}

	startTimer := func(timer *time.Timer, running *bool, d time.Duration) {
		if !*running {
			timer.Reset(d)
			*running = true
		}
	}

	stopTimer := func(timer *time.Timer, running *bool) {
		if *running && !timer.Stop() {
			<-timer.C
		}
		*running = false
	}

	sendTimerRunning := false
	sendTimer := createStoppedTimer()

	recvTimerRunning := false
	recvTimer := createStoppedTimer()

	defer func() {
		go func() {
			chain.senderWaitGroup.Wait()
			close(chain.timeToCutRequestChan)
		}()

		stopTimer(sendTimer, &sendTimerRunning)
		stopTimer(recvTimer, &recvTimerRunning)

		for {
			// drain pending requests
			_, ok := <-chain.timeToCutRequestChan
			if !ok {
				break
			}
		}

		if err := chain.topicProducer.Close(); err != nil {
			logger.Errorf("[channel: %s] error when closing topicProducer = %v", chain.ChannelID(), err)
		}
	}()

	var messageHash []byte
	var blockNumber uint64
	for {
		select {
		case <-chain.doneProcessingMessages:
			return
		case request := <-chain.timeToCutRequestChan:
			if request.messageHash != nil {
				// from sender
				if !sendTimerRunning {
					startTimer(sendTimer, &sendTimerRunning, chain.SharedConfig().BatchTimeout())
					messageHash = request.messageHash
				}
			} else {
				// triggered by receive side
				switch {
				case recvTimerRunning && !request.start:
					// timer is running and request to stop
					stopTimer(recvTimer, &recvTimerRunning)
				case !recvTimerRunning && request.start:
					// timer is not already running and request to start
					startTimer(recvTimer, &recvTimerRunning, chain.SharedConfig().BatchTimeout())
					blockNumber = request.nextBlockNumber
				default:
					// do nothing for other cases
				}
			}
		case <-sendTimer.C:
			sendTimerRunning = false
			if err := chain.sendTimeToCut(newTimeToCutMessageWithMessageHash(messageHash)); err != nil {
				logger.Errorf("[channel: %s] cannot send time-to-cut message with messageHash: %s", chain.ChannelID(), err)
			} else {
				logger.Infof("[channel: %s] time-to-cut with messageHash sent", chain.ChannelID())
			}
		case <-recvTimer.C:
			recvTimerRunning = false
			if err := chain.sendTimeToCut(newTimeToCutMessageWithBlockNumber(blockNumber)); err != nil {
				logger.Errorf("[channel: %s] failed with block number %d: %s", chain.ChannelID(), blockNumber, err)
			} else {
				logger.Infof("[channel: %s] time-to-cut with block number %d sent", chain.ChannelID(), blockNumber)
			}
		}
	}
}

func (chain *chainImpl) processMessages() error {
	defer func() {
		chain.topicSubscriptionHandle.Unsubscribe()
		if err := chain.topicConsumer.Close(); err != nil {
			logger.Errorf("[channel: %s] error when closing topicConsumer = %v", chain.ChannelID(), err)
		}
		close(chain.doneProcessingMessages)

		select {
		case <-chain.errorChan: // If already closed, don't do anything
		default:
			close(chain.errorChan)
		}

		if !chain.subscriptionRetryTimer.Stop() {
			select {
			case <-chain.subscriptionRetryTimer.C:
			default:
				// do nothing if already stopped
			}
		}
	}()

	recollectPendingChunks := !chain.lastConsensusTimestampPersisted.Equal(chain.lastChunkFreeConsensusTimestamp)
	if recollectPendingChunks {
		// recollect chunks pending reassembly at the time of last shutdown / crash, handle it by adding all messages
		// in the range (lastChunkFreeBlock.LastConsensusTimestampPersisted, chain.LastConsensusTimestampPersisted] to appMsgProcessor
		logger.Infof("[channel: %s] going to collect chunks pending reassembly at the time of last shutdown / crash", chain.ChannelID())
	} else {
		logger.Infof("[channel: %s] going into the normal message processing loop", chain.ChannelID())
	}
	subscriptionRetryCount := 0
	scheduleSubscriptionRetry := func() {
		delay := time.Duration(float64(subscriptionRetryBaseDelay) * math.Pow(2, float64(subscriptionRetryCount)))
		chain.subscriptionRetryTimer.Reset(delay)
		logger.Infof("[channel: %s] retry topic subscription in %dms", chain.ChannelID(), delay.Milliseconds())
	}
	for {
		select {
		case <-chain.haltChan:
			logger.Warningf("[channel: %s] consenter for channel exiting", chain.ChannelID())
			return nil
		case hcsErr := <-chain.topicSubscriptionHandle.Errors():
			logger.Errorf("[channel: %s] error received during subscription streaming: %s", chain.ChannelID(), hcsErr)
			select {
			case <-chain.errorChan: // don't do anything if already closed
			default:
				logger.Errorf("[channel: %s] closing errorChan due to subscription streaming error", chain.ChannelID())
				close(chain.errorChan)
			}
			st, ok := status.FromError(hcsErr)
			if ok && isSubscriptionErrorRecoverable(st.Code()) && subscriptionRetryCount < subscriptionRetryMax {
				// the error is recoverable and the max retry isn't reached yet
				scheduleSubscriptionRetry()
			} else {
				logger.Errorf("[channel: %s] closing haltChan due to subscription streaming error", chain.ChannelID())
				close(chain.haltChan)
			}
		case <-chain.subscriptionRetryTimer.C:
			logger.Infof("[channel: %s] retry topic (%s) subscription", chain.ChannelID(), chain.topicID)
			subscriptionRetryCount++
			failedSubscriptionHandle := chain.topicSubscriptionHandle
			if err := startSubscription(chain, chain.lastConsensusTimestamp); err != nil {
				logger.Errorf("[channel: %s] startSubscription failed, retry count %d: %v", chain.ChannelID(), subscriptionRetryCount, err)
				if subscriptionRetryCount < subscriptionRetryMax {
					scheduleSubscriptionRetry()
				} else {
					logger.Errorf("[channel: %s] closing haltChan due to subscription streaming error", chain.ChannelID())
					close(chain.haltChan)
				}
			} else {
				// only unsubscribe when new subscription handle is created successfully
				failedSubscriptionHandle.Unsubscribe()
				logger.Infof("[channel: %s] new subscription created, send a stale time-to-cut message to trigger response just in case none is pending", chain.ChannelID())
				// send a stale ttc to resume response handling, in case of no other pending responses
				if err := chain.sendTimeToCut(newTimeToCutMessageWithBlockNumber(chain.lastCutBlockNumber)); err != nil {
					logger.Errorf("[channel: %s] failed to send a stale time-to-cut message with block number %d: %s", chain.ChannelID(), chain.lastCutBlockNumber, err)
				}
			}
		case resp, ok := <-chain.topicSubscriptionHandle.Responses():
			if !ok {
				logger.Criticalf("[channel: %s] hcs topic subscription closed", chain.ChannelID())
				return nil
			}
			subscriptionRetryCount = 0
			select {
			case <-chain.errorChan:
				chain.errorCMutex.Lock()
				chain.errorChan = make(chan struct{}) // make a new one, make the chain available again
				chain.errorCMutex.Unlock()
				logger.Infof("[channel: %s] marked chain as available again", chain.ChannelID())
			default:
			}

			if resp.SequenceNumber != chain.lastSequenceProcessed+1 {
				logger.Panicf("[channel: %s] incorrect sequence number (%d), expect (%d), exiting...", chain.ChannelID(), resp.SequenceNumber, chain.lastSequenceProcessed+1)
			}
			if !resp.ConsensusTimeStamp.After(chain.lastConsensusTimestamp) {
				logger.Panicf("[channel: %s] resp.ConsensusTimestamp(%d) not after lastConsensusTimestamp(%d)", chain.ChannelID(), resp.ConsensusTimeStamp.UnixNano(), chain.lastConsensusTimestamp.UnixNano())
			}
			chain.lastSequenceProcessed++
			chain.lastConsensusTimestamp = resp.ConsensusTimeStamp
			chunk := new(hb.ApplicationMessageChunk)
			if err := proto.Unmarshal(resp.Message, chunk); err != nil {
				logger.Errorf("[channel: %s] failed to unmarshal ordered message into ApplicationMessageChunk = %v", chain.ChannelID(), err)
				continue
			}
			logger.Debugf("[channel: %s] processing a chunk with consensus timestamp = %d, sequence = %d", chain.ChannelID(), resp.ConsensusTimeStamp.UnixNano(), resp.SequenceNumber)
			payload, msgHash, expiredMessages, expiredChunks, err := chain.appMsgProcessor.Reassemble(chunk, resp.ConsensusTimeStamp)
			if !chain.appMsgProcessor.IsPending() {
				chain.lastChunkFreeConsensusTimestamp = chain.lastConsensusTimestamp
				chain.lastChunkFreeSequenceProcessed = chain.lastSequenceProcessed
			}
			if err != nil {
				logger.Errorf("[channel: %s] failed to process a received chunk - %v", chain.ChannelID(), err)
				continue
			}
			if expiredMessages != 0 {
				logger.Warnf("[channel: %s] %d messages and %d chunks expired", chain.ChannelID(), expiredMessages, expiredChunks)
				chain.consenter.Metrics().NumberMessagesDropped.With("channel", chain.ChannelID()).Add(float64(expiredMessages))
				chain.consenter.Metrics().NumberChunksDropped.With("channel", chain.ChannelID()).Add(float64(expiredChunks))
			}
			if payload == nil {
				logger.Debugf("need more chunks to reassemble the HCS message")
				continue
			}
			logger.Debugf("[channel: %s] reassembled a message of %d bytes", chain.ChannelID(), len(payload))
			msg := new(hb.HcsMessage)
			if err := proto.Unmarshal(payload, msg); err != nil {
				logger.Criticalf("[channel: %s] unable to unmarshal ordered message", chain.ChannelID())
				continue
			}
			logger.Debugf("[channel %s] successfully unmarshaled ordered message, consensus timestamp %d",
				chain.ChannelID(), resp.ConsensusTimeStamp.UnixNano())
			if !recollectPendingChunks {
				// use ConsenusTimestamp and SequenceNumber of the last received chunk for that of a message
				switch msg.Type.(type) {
				case *hb.HcsMessage_Regular:
					if err := chain.processRegularMessage(msg.GetRegular(), resp.ConsensusTimeStamp, resp.SequenceNumber, msgHash); err != nil {
						logger.Warningf("[channel: %s] error when processing incoming message of type REGULAR = %s", chain.ChannelID(), err)
					}
				case *hb.HcsMessage_TimeToCut:
					if err := chain.processTimeToCutMessage(msg.GetTimeToCut(), resp.ConsensusTimeStamp, resp.SequenceNumber); err != nil {
						logger.Criticalf("[channel: %s] consenter for channel exiting, %s", chain.ChannelID(), err)
						return err
					}
				case *hb.HcsMessage_OrdererStarted:
					chain.processOrdererStartedMessage(msg.GetOrdererStarted())
				}
			} else {
				if resp.ConsensusTimeStamp.Equal(chain.lastConsensusTimestampPersisted) {
					recollectPendingChunks = false
					logger.Debugf("[channel: %s] switching to the normal message processing loop", chain.ChannelID())
					if chain.lastResubmittedConfigSequence == 0 || chain.lastResubmittedConfigSequence <= chain.lastOriginalSequenceProcessed {
						// unblock ingress
						logger.Debugf("[channel: %s] unblock ingress", chain.ChannelID())
						chain.reprocessComplete()
					}
				} else if resp.ConsensusTimeStamp.After(chain.lastConsensusTimestampPersisted) {
					logger.Panicf("[channel: %s] consensus timestamp (%d) of last processed message is later "+
						"than chain.lastConsensusTimestampPersisted (%d)", chain.ChannelID(), resp.ConsensusTimeStamp.UnixNano(),
						chain.lastConsensusTimestampPersisted.UnixNano())
				}
			}
		}
	}
}

func (chain *chainImpl) WriteBlock(block *cb.Block, isConfig bool, consensusTimestamp time.Time) {
	if !isConfig {
		// clear the map when writing a non-config block
		chain.messageHashes = make(map[string]struct{})
	}
	chain.timeToCutRequestChan <- timeToCutRequest{start: false}
	logger.Infof("[channel: %s] request to stop the batch timer since a block is cut", chain.ChannelID())

	chain.lastCutBlockNumber++
	metadata := newHcsMetadata(
		timestampProtoOrPanic(consensusTimestamp),
		chain.lastOriginalSequenceProcessed,
		chain.lastResubmittedConfigSequence,
		timestampProtoOrPanic(chain.lastChunkFreeConsensusTimestamp),
		chain.lastChunkFreeSequenceProcessed)
	marshaledMetadata := protoutil.MarshalOrPanic(metadata)
	if !isConfig {
		chain.ConsenterSupport.WriteBlock(block, marshaledMetadata)
	} else {
		chain.ConsenterSupport.WriteConfigBlock(block, marshaledMetadata)
	}
	chain.consenter.Metrics().CommittedBlockNumber.With("channel", chain.ChannelID()).Set(float64(chain.lastCutBlockNumber))
	chain.consenter.Metrics().LastConsensusTimestampPersisted.With("channel", chain.ChannelID()).Set(float64(consensusTimestamp.UnixNano()))
}

func (chain *chainImpl) commitNormalMessage(message *cb.Envelope, curConsensusTimestamp time.Time, newOriginalSequenceProcessed uint64, messageHash []byte) {
	var pending bool
	defer func() {
		if pending {
			// if pending, request to send time-to-cut message
			chain.timeToCutRequestChan <- timeToCutRequest{
				start:           true,
				nextBlockNumber: chain.lastCutBlockNumber + 1,
			}
			logger.Debugf("[channel: %s] request to send time-to-cut with block number %d", chain.ChannelID(), chain.lastCutBlockNumber+1)

			// record hash as the message is pending in blockcutter
			chain.messageHashes[string(messageHash)] = struct{}{}
		}
	}()

	batches, pending := chain.BlockCutter().Ordered(message)
	logger.Debugf("[channel: %s] Ordering results: items in batch = %d, pending = %v", chain.ChannelID(), len(batches), pending)

	if len(batches) == 0 {
		// If no block is cut, we update the `lastOriginalSequenceProcessed`, start the timer if necessary and return
		chain.lastOriginalSequenceProcessed = newOriginalSequenceProcessed
		chain.lastOrdereredConsensusTimestamp = curConsensusTimestamp
		return
	}

	if pending || len(batches) == 2 {
		// If the newest envelope is not encapsulated into the first batch,
		// the `LastConsensusTimestampPersisted` should be `chain.lastOrdereredConsensusTimestamp`
		// the 'LastOriginalSequenceProcessed` should be `chain.lastOriginalSequenceProcessed`
	} else {
		// We are just cutting exactly one block, so it is safe to update
		// `lastOriginalSequenceProcessed` with `newOriginalSequenceProcessed` here, and then
		// encapsulate it into this block. Otherwise, if we are cutting two
		// blocks, the first one should use current `lastOriginalSequenceProcessed`
		// and the second one should use `newOriginalSequenceProcessed`, which is also used to
		// update `lastOriginalSequenceProcessed`
		chain.lastOriginalSequenceProcessed = newOriginalSequenceProcessed
		chain.lastOrdereredConsensusTimestamp = curConsensusTimestamp
	}

	// Commit the first block
	block := chain.CreateNextBlock(batches[0])
	chain.WriteBlock(block, false, chain.lastOrdereredConsensusTimestamp)
	logger.Infof("[channel: %s] Batch filled, just cut block [%d] - last persisted consensus timestamp"+
		" is now %d", chain.ChannelID(), chain.lastCutBlockNumber, chain.lastConsensusTimestamp.UnixNano())

	// Commit the second block if exists
	if len(batches) == 2 {
		chain.lastOriginalSequenceProcessed = newOriginalSequenceProcessed
		chain.lastOrdereredConsensusTimestamp = curConsensusTimestamp
		block := chain.CreateNextBlock(batches[1])
		chain.WriteBlock(block, false, chain.lastOrdereredConsensusTimestamp)
		logger.Infof("[channel: %s] Batch filled, just cut block [%d] - last persisted consensus "+
			"timestamp is now %d", chain.ChannelID(), chain.lastCutBlockNumber, chain.lastOrdereredConsensusTimestamp.UnixNano())
	}
}

func (chain *chainImpl) commitConfigMessage(message *cb.Envelope, curConsensusTimestamp time.Time, newOriginalSequenceProcessed uint64) {
	logger.Debugf("[channel: %s] Received config message", chain.ChannelID())
	batch := chain.BlockCutter().Cut()

	if batch != nil {
		logger.Debugf("[channel: %s] Cut pending messages into block", chain.ChannelID())
		block := chain.CreateNextBlock(batch)
		chain.WriteBlock(block, false, chain.lastOrdereredConsensusTimestamp)
	}

	logger.Debugf("[channel: %s] Creating isolated block for config message", chain.ChannelID())
	chain.lastOriginalSequenceProcessed = newOriginalSequenceProcessed
	block := chain.CreateNextBlock([]*cb.Envelope{message})
	chain.WriteBlock(block, true, curConsensusTimestamp)
}

func (chain *chainImpl) processRegularMessage(msg *hb.HcsMessageRegular, timestamp time.Time, receivedSequence uint64, messageHash []byte) error {
	curConfigSeq := chain.Sequence()
	env := &cb.Envelope{}
	if err := proto.Unmarshal(msg.Payload, env); err != nil {
		// This shouldn't happen, it should be filtered at ingress
		err = errors.Errorf("failed to unmarshal payload of regular message because = %s", err)
		logger.Errorf("[channel: %s] %v", chain.ChannelID(), err)
		return err
	}

	logger.Debugf("[channel: %s] Processing regular HCS message of type %s with ConfigSeq %d, curConfigSeq %d",
		chain.ChannelID(), msg.Class.String(), msg.ConfigSeq, curConfigSeq)

	switch msg.Class {
	case hb.HcsMessageRegular_NORMAL:
		// This is a message that is re-validated and re-ordered
		if msg.OriginalSeq != 0 {
			logger.Debugf("[channel: %s] Received re-submitted normal message with original sequence %d", chain.ChannelID(), msg.OriginalSeq)

			// But we've reprocessed it already
			if msg.OriginalSeq <= chain.lastOriginalSequenceProcessed {
				logger.Debugf(
					"[channel: %s] OriginalSeq(%d) <= lastOriginalSequenceProcessed(%d), message has been consumed already, discard",
					chain.ChannelID(), msg.OriginalSeq, chain.lastOriginalSequenceProcessed)
				return nil
			}

			logger.Debugf(
				"[channel: %s] OriginalSeq(%d) > lastOriginalSequenceProcessed(%d), "+
					"this is the first time we receive this re-submitted normal message",
				chain.ChannelID(), msg.OriginalSeq, chain.lastOriginalSequenceProcessed)

			// In case we haven't reprocessed the message, there's no need to differentiate it from those
			// messages that will be processed for the first time.
		}

		// The configuration has changed
		if msg.ConfigSeq < curConfigSeq {
			logger.Debugf("[channel: %s] config sequence has advanced since this normal message got validated, re-validating", chain.ChannelID())
			configSeq, err := chain.ProcessNormalMsg(env)
			if err != nil {
				return fmt.Errorf("discarding bad normal message because = %s", err)
			}

			logger.Debugf("[channel: %s] Normal message is still valid, re-submit", chain.ChannelID())

			// For both messages that are ordered for the first time or re-ordered, we set original offset
			// to current received offset and re-order it.
			if err := chain.order(env, configSeq, receivedSequence); err != nil {
				return fmt.Errorf("error re-submitting normal message because = %s", err)
			}

			return nil
		}

		// Any messages coming in here may or may not have been re-validated
		// and re-ordered, BUT they are definitely valid here

		// advance lastOriginalOffsetProcessed if message is re-validated and re-ordered
		newSeq := msg.OriginalSeq
		if newSeq == 0 {
			newSeq = chain.lastOriginalSequenceProcessed
		}

		chain.commitNormalMessage(env, timestamp, newSeq, messageHash)

	case hb.HcsMessageRegular_CONFIG:
		// This is a message that is re-validated and re-ordered
		if msg.OriginalSeq != 0 {
			logger.Debugf("[channel: %s] Received re-submitted config message with original offset %d", chain.ChannelID(), msg.OriginalSeq)

			// But we've reprocessed it already
			if msg.OriginalSeq <= chain.lastOriginalSequenceProcessed {
				logger.Debugf(
					"[channel: %s] OriginalSeq(%d) <= lastOriginalSequenceProcessed(%d), message has been consumed already, discard",
					chain.ChannelID(), msg.OriginalSeq, chain.lastOriginalSequenceProcessed)
				return nil
			}

			logger.Debugf(
				"[channel: %s] OriginalSeq(%d) > lastOriginalSequenceProcessed(%d), "+
					"this is the first time we receive this re-submitted config message",
				chain.ChannelID(), msg.OriginalSeq, chain.lastOriginalSequenceProcessed)

			if msg.OriginalSeq == chain.lastResubmittedConfigSequence && // This is very last resubmitted config message
				msg.ConfigSeq == curConfigSeq { // AND we don't need to resubmit it again
				logger.Debugf("[channel: %s] Config message with original offset %d is the last in-flight resubmitted message"+
					"and it does not require revalidation, unblock ingress messages now", chain.ChannelID(), msg.OriginalSeq)
				chain.reprocessComplete() // Therefore, we could finally unblock broadcast
			}

			// Somebody resubmitted message at offset X, whereas we didn't. This is due to non-determinism where
			// that message was considered invalid by us during re-validation, however somebody else deemed it to
			// be valid, and resubmitted it. We need to advance lastResubmittedConfigOffset in this case in order
			// to enforce consistency across the network.
			if chain.lastResubmittedConfigSequence < msg.OriginalSeq {
				chain.lastResubmittedConfigSequence = msg.OriginalSeq
			}
		}

		// The config sequence has advanced
		if msg.ConfigSeq < curConfigSeq {
			logger.Debugf("[channel: %s] Config sequence has advanced since this config message got validated, re-validating", chain.ChannelID())
			configEnv, configSeq, err := chain.ProcessConfigMsg(env)
			if err != nil {
				return fmt.Errorf("rejecting config message because = %s", err)
			}

			// For both messages that are ordered for the first time or re-ordered, we set original offset
			// to current received offset and re-order it.
			if err := chain.configure(configEnv, configSeq, receivedSequence); err != nil {
				return fmt.Errorf("error re-submitting config message because = %s", err)
			}

			logger.Debugf("[channel: %s] Resubmitted config message with sequence %d, block ingress messages", chain.ChannelID(), receivedSequence)
			chain.lastResubmittedConfigSequence = receivedSequence // Keep track of last resubmitted message offset
			chain.reprocessPending()                               // Begin blocking ingress messages

			return nil
		}

		// Any messages coming in here may or may not have been re-validated
		// and re-ordered, BUT they are definitely valid here

		// advance lastOriginalOffsetProcessed if message is re-validated and re-ordered
		newSeq := msg.OriginalSeq
		if newSeq == 0 {
			newSeq = chain.lastOriginalSequenceProcessed
		}

		chain.commitConfigMessage(env, timestamp, newSeq)
		// new configuration is applied, number of orderers may have changed
		chain.consenter.Metrics().NumberNodes.With("channel", chain.ChannelID()).Set(float64(len(chain.ChannelConfig().OrdererAddresses())))

		// update publicKeys
		hcsConfigMetadata := &hb.HcsConfigMetadata{}
		if err := proto.Unmarshal(chain.SharedConfig().ConsensusMetadata(), hcsConfigMetadata); err != nil {
			logger.Panicf("[channel: %s] invalid consensus metadata in new channel configuration - %v", chain.ChannelID(), err)
		}
		publicKeys, err := getPublicKeys(hcsConfigMetadata.PublicKeys, chain.operatorPrivateKey.PublicKey())
		if err != nil {
			logger.Panicf("[channel: %s] failed to parse public keys in consensus metadata - %v", chain.ChannelID(), err)
		}
		chain.publicKeys = publicKeys
		logger.Debugf("[channel: %s] channel configuration has changed, publicKeys gets re-parsed", chain.ChannelID())
	default:
		return errors.Errorf("unsupported regular HCS message type: %v", msg.Class.String())
	}

	return nil
}

func (chain *chainImpl) processTimeToCutMessage(msg *hb.HcsMessageTimeToCut, timestamp time.Time, sequence uint64) error {
	var createBlock bool
	var trigger string

	switch msg.Request.(type) {
	case *hb.HcsMessageTimeToCut_BlockNumber:
		blockNumber := msg.GetBlockNumber()
		if blockNumber == chain.lastCutBlockNumber+1 {
			createBlock = true
			trigger = fmt.Sprintf("blocknumber %d", blockNumber)
		} else if blockNumber > chain.lastCutBlockNumber+1 {
			return fmt.Errorf("discard larger time-to-cut message (%d) than expected (%d)", blockNumber, chain.lastCutBlockNumber+1)
		} else {
			logger.Debugf("[channel: %s] ignore stale/late time-to-cut (block %d)", chain.ChannelID(), blockNumber)
		}
	case *hb.HcsMessageTimeToCut_MessageHash:
		if _, ok := chain.messageHashes[string(msg.GetMessageHash())]; ok {
			createBlock = true
			trigger = "messagehash"
		} else {
			logger.Debugf("[channel: %s] ignore stale/late time-to-cut with messagehash", chain.ChannelID())
		}
	}

	if createBlock {
		logger.Infof("[channel: %s] received correct time-to-cut-message with %s, try to cut a block", chain.ChannelID(), trigger)
		batch := chain.BlockCutter().Cut()
		if len(batch) == 0 {
			return fmt.Errorf("bug, got correct time-to-cut message, but no pending transactions")
		}
		block := chain.CreateNextBlock(batch)
		chain.lastOriginalSequenceProcessed = sequence
		chain.WriteBlock(block, false, timestamp)
		logger.Infof("[channel: %s] successfully cut block, triggered by time-to-cut with %s", chain.ChannelID(), trigger)
	}
	return nil
}

func (chain *chainImpl) processOrdererStartedMessage(msg *hb.HcsMessageOrdererStarted) {
	logger.Infof("[channel: %s] orderer %s just started", chain.ChannelID(), hex.EncodeToString(msg.OrdererIdentity))
	if expiredMessages, expiredChunks, err := chain.appMsgProcessor.ExpireByAppID(msg.OrdererIdentity); err == nil {
		logger.Infof("[channel: %s] %d pending messages (%d pending chunks) from orderer %s dropped", chain.ChannelID(), expiredMessages, expiredChunks, hex.EncodeToString(msg.OrdererIdentity))
		chain.consenter.Metrics().NumberMessagesDropped.With("channel", chain.ChannelID()).Add(float64(expiredMessages))
		chain.consenter.Metrics().NumberChunksDropped.With("channel", chain.ChannelID()).Add(float64(expiredChunks))
	} else {
		logger.Errorf("[channel: %s] ExpireByAppID returns error = %v", chain.ChannelID(), err)
	}
}

func (chain *chainImpl) sendTimeToCut(msg *hb.HcsMessage) error {
	if ok, _ := chain.enqueue(msg, false); !ok {
		return errors.Errorf("[channel: %s] failed to send time-to-cut message", chain.ChannelID())
	}
	return nil
}

func (chain *chainImpl) order(env *cb.Envelope, configSeq uint64, originalOffset uint64) error {
	var msgHash []byte
	defer func() {
		if msgHash != nil {
			// request to start the batch timeout timer when a non-config message is successfully sent
			chain.timeToCutRequestChan <- timeToCutRequest{messageHash: msgHash}
			logger.Debugf("[channel: %s] request to start the batch timer with non-config message successfully sent", chain.ChannelID())
		}
		chain.senderWaitGroup.Done()
	}()

	chain.senderWaitGroup.Add(1)
	marshaledEnv, err := protoutil.Marshal(env)
	if err != nil {
		return errors.Errorf("cannot enqueue, unable to marshal envelope: %s", err)
	}
	ok, msgHash := chain.enqueue(newNormalMessage(marshaledEnv, configSeq, originalOffset), originalOffset != 0)
	if !ok {
		return errors.Errorf("[channel: %s] cannot enqueue", chain.ChannelID())
	}
	return nil
}

func (chain *chainImpl) configure(config *cb.Envelope, configSeq uint64, originalOffset uint64) error {
	defer chain.senderWaitGroup.Done()
	chain.senderWaitGroup.Add(1)

	marshaledConfig, err := protoutil.Marshal(config)
	if err != nil {
		return errors.Errorf("unable to marshal config because %s", err)
	}
	if ok, _ := chain.enqueue(newConfigMessage(marshaledConfig, configSeq, originalOffset), originalOffset != 0); !ok {
		return fmt.Errorf("cannot enqueue the config message")
	}
	return nil
}

func (chain *chainImpl) enqueue(message *hb.HcsMessage, isResubmission bool) (bool, []byte) {
	logger.Debugf("[channel: %s] Enqueueing envelope...", chain.ChannelID())
	select {
	case <-chain.startChan: // The Start phase has completed
		select {
		case <-chain.haltChan: // The chain has been halted, stop here
			logger.Warningf("[channel: %s] consenter for this channel has been halted", chain.ChannelID())
			return false, nil
		default: // The post path
			return chain.enqueueChecked(message, isResubmission)
		}
	default: // Not ready yet
		logger.Warningf("[channel: %s] Will not enqueue, consenter for this channel hasn't started yet", chain.ChannelID())
		return false, nil
	}
}

func (chain *chainImpl) enqueueChecked(message *hb.HcsMessage, isResubmission bool) (bool, []byte) {
	payload, err := protoutil.Marshal(message)
	if err != nil {
		logger.Errorf("[channel: %s] unable to marshal HCS message because = %s", chain.ChannelID(), err)
		return false, nil
	}
	chunks, msgHash, err := chain.appMsgProcessor.Split(payload, chain.consenter.getUniqueValidStart(time.Now().Add(-10*time.Second)))
	if err != nil {
		logger.Errorf("[channel: %s] failed to split message - %v", chain.ChannelID(), err)
		return false, nil
	}
	logger.Debugf("[channel: %s] the payload of %d bytes is cut into %d chunks, resubmission ? %v",
		chain.ChannelID(), len(payload), len(chunks), isResubmission)
	for _, chunk := range chunks {
		for attempt := 0; attempt < 3; attempt++ {
			txID := hedera.TransactionID{
				AccountID:  *chain.operatorID,
				ValidStart: chain.consenter.getUniqueValidStart(time.Now().Add(-10 * time.Second)),
			}
			_, err = chain.topicProducer.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunk), chain.topicID, &txID)
			if err != nil {
				switch e := err.(type) {
				case hedera.ErrHederaPreCheckStatus:
					if e.Status == hedera.StatusDuplicateTransaction || e.Status == hedera.StatusBusy {
						logger.Warnf("[channel: %s] received PreCheckStatus %s for tx %s, will retry", chain.ChannelID(), e.Status, txID)
						continue
					}
				case hedera.ErrHederaNetwork:
					if e.StatusCode != nil && *e.StatusCode == codes.Internal {
						logger.Warnf("[channel: %s] received ErrHederaNetwork %s for tx %s, will retry", chain.ChannelID(), e, txID)
						continue
					}
				default:
					// fall through for other errors
				}
			}
			break
		}
		if err != nil {
			logger.Errorf("[channel: %s] failed to send chunk %d, abort the whole message: %s", chain.ChannelID(), chunk.ChunkIndex, err)
			return false, nil
		}
		logger.Debugf("[channel: %s] chunk %d of message sent successfully", chain.ChannelID(), chunk.ChunkIndex)
	}
	logger.Debugf("[channel: %s] Envelope enqueued successfully", chain.ChannelID())
	return true, msgHash
}

func (chain *chainImpl) doneReprocessing() <-chan struct{} {
	chain.doneReprocessingMutex.Lock()
	defer chain.doneReprocessingMutex.Unlock()
	return chain.doneReprocessingMsgInFlight
}

func (chain *chainImpl) reprocessComplete() {
	chain.doneReprocessingMutex.Lock()
	defer chain.doneReprocessingMutex.Unlock()
	close(chain.doneReprocessingMsgInFlight)
}

func (chain *chainImpl) reprocessPending() {
	chain.doneReprocessingMutex.Lock()
	defer chain.doneReprocessingMutex.Unlock()
	chain.doneReprocessingMsgInFlight = make(chan struct{})
}

func (chain *chainImpl) HealthCheck(ctx context.Context) error {
	failed := make([]string, 0, len(chain.network))
	for address, nodeID := range chain.network {
		if err := chain.topicProducer.Ping(&nodeID); err != nil {
			failed = append(failed, address)
		}
	}
	if len(failed) != 0 {
		return fmt.Errorf("cannot connect to: %s", strings.Join(failed, ", "))
	}
	return nil
}

func startThread(chain *chainImpl) {
	var err error

	// create topicProducer
	chain.topicProducer, err = chain.hcf.GetConsensusClient(chain.network, chain.operatorID, chain.operatorPrivateKey)
	if err != nil {
		logger.Panicf("[channel: %s] failed to set up topic producer, %s", chain.ChannelID(), err)
	}

	// create topicConsumer and start subscription
	chain.topicConsumer, err = chain.hcf.GetMirrorClient(chain.consenter.sharedHcsConfig().MirrorNodeAddress)
	if err != nil {
		logger.Panicf("[channel: %s] failed to set up topic consumer, %s", chain.ChannelID(), err)
	}
	logger.Debugf("[channel: %s] created topic consumer with mirror node address %s",
		chain.ChannelID(), chain.consenter.sharedHcsConfig().MirrorNodeAddress)

	// subscribe to the hcs topic
	logger.Infof("[channel: %s] the HCS topic ID is %v", chain.ChannelID(), chain.topicID)
	if chain.lastChunkFreeConsensusTimestamp.After(chain.lastConsensusTimestampPersisted) {
		logger.Panicf("[channel: %s] corrupted metadata? chain.lastChunkFreeConsensusTimestamp is after "+
			"chain.lastConsensusTimestampPersisted", chain.ChannelID())
	}
	err = startSubscription(chain, chain.lastConsensusTimestamp)
	if err != nil {
		logger.Panicf("[channel: %s] failed to start topic subscription = %v", chain.ChannelID(), err)
	}

	chain.doneProcessingMessages = make(chan struct{})

	if ok, _ := chain.enqueueChecked(newOrdererStartedMessage(chain.appID), false); !ok {
		logger.Panicf("[channel: %s] failed to send orderer started message", chain.ChannelID())
	}

	chain.errorCMutex.Lock()
	close(chain.startChan)
	chain.errorChan = make(chan struct{})
	chain.errorCMutex.Unlock()
	logger.Infof("[channel: %s] Start phase completed successfully", chain.ChannelID())

	chain.subscriptionRetryTimer = time.NewTimer(time.Millisecond)
	if !chain.subscriptionRetryTimer.Stop() {
		<-chain.subscriptionRetryTimer.C
	}

	go chain.processTimeToCutRequests()

	if err = chain.processMessages(); err != nil {
		logger.Errorf("[channel: %s] processMessages exited with error: %s", chain.ChannelID(), err)
	}
}

func getPublicKeys(publicKeysIn []*hb.HcsConfigPublicKey, extra hedera.Ed25519PublicKey) (map[string]*hedera.Ed25519PublicKey, error) {
	publicKeys := map[string]*hedera.Ed25519PublicKey{
		string(extra.Bytes()): &extra,
	}
	for _, publicKeyIn := range publicKeysIn {
		if publicKeyIn.Type != "ed25519" {
			return nil, fmt.Errorf("unsupported public key type %s", publicKeyIn.Type)
		}
		publicKey, err := hedera.Ed25519PublicKeyFromString(publicKeyIn.Key)
		if err != nil {
			return nil, err
		}
		publicKeys[string(publicKey.Bytes())] = &publicKey
	}
	return publicKeys, nil
}

func parseConfig(
	configMetaData []byte,
	config *localconfig.Hcs,
) (topicID hedera.ConsensusTopicID, publicKeys map[string]*hedera.Ed25519PublicKey, reassembleTimeout time.Duration, network map[string]hedera.AccountID, operatorID *hedera.AccountID, privateKey *hedera.Ed25519PrivateKey, err error) {
	hcsConfigMetadata := &hb.HcsConfigMetadata{}
	if config.Operator.PrivateKey.Type != "ed25519" {
		err = fmt.Errorf("private key type \"%s\" is not supported", config.Operator.PrivateKey.Type)
		return
	}
	tmpPrivateKey, err := parseEd25519PrivateKey(config.Operator.PrivateKey.Key)
	if err != nil {
		err = fmt.Errorf("invalid operator private key = %v", err)
		return
	}

	if err = proto.Unmarshal(configMetaData, hcsConfigMetadata); err != nil {
		return
	}
	tmpTopicID, err := hedera.TopicIDFromString(hcsConfigMetadata.TopicId)
	if err != nil {
		return
	}

	tmpPublicKeys, err := getPublicKeys(hcsConfigMetadata.PublicKeys, tmpPrivateKey.PublicKey())
	if err != nil {
		err = fmt.Errorf("failed to parse public keys = %v", err)
		return
	}

	timeout, err := time.ParseDuration(hcsConfigMetadata.ReassembleTimeout)
	if err != nil {
		err = fmt.Errorf("failed to parse reassemble timeout = %v", err)
		return
	}
	if timeout == 0 {
		err = fmt.Errorf("invalid reassemble timeout - %s", timeout)
		return
	}

	nodes := config.Nodes
	if len(nodes) == 0 {
		err = fmt.Errorf("empty nodes list in hcs config")
		return
	}
	tmpNetwork := make(map[string]hedera.AccountID)
	for addr, acc := range nodes {
		accountID, err1 := hedera.AccountIDFromString(acc)
		if err1 != nil {
			err = fmt.Errorf("invalid account ID = %v", err1)
			return
		}
		tmpNetwork[addr] = accountID
	}

	tmpOperatorID, err := hedera.AccountIDFromString(config.Operator.Id)
	if err != nil {
		err = fmt.Errorf("invalid operator ID = %v", err)
		return
	}

	topicID = tmpTopicID
	publicKeys = tmpPublicKeys
	reassembleTimeout = timeout
	network = tmpNetwork
	operatorID = &tmpOperatorID
	privateKey = &tmpPrivateKey
	return
}

func parseEd25519PrivateKey(str string) (hedera.Ed25519PrivateKey, error) {
	privateKey, err := hedera.Ed25519PrivateKeyFromString(str)
	if err != nil {
		privateKey, err = hedera.Ed25519PrivateKeyFromPem([]byte(str), "")
	}
	return privateKey, err
}

func startSubscription(chain *chainImpl, startTime time.Time) error {
	if !startTime.Equal(unixEpoch) {
		startTime = startTime.Add(time.Nanosecond)
	}
	handle, err := chain.topicConsumer.SubscribeTopic(chain.topicID, &startTime, nil)
	if err != nil {
		return err
	}
	chain.topicSubscriptionHandle = handle
	return nil
}

func newConfigMessage(config []byte, configSeq uint64, originalSeq uint64) *hb.HcsMessage {
	return &hb.HcsMessage{
		Type: &hb.HcsMessage_Regular{
			Regular: &hb.HcsMessageRegular{
				Payload:     config,
				ConfigSeq:   configSeq,
				Class:       hb.HcsMessageRegular_CONFIG,
				OriginalSeq: originalSeq,
			},
		},
	}
}

func newNormalMessage(payload []byte, configSeq uint64, originalSeq uint64) *hb.HcsMessage {
	return &hb.HcsMessage{
		Type: &hb.HcsMessage_Regular{
			Regular: &hb.HcsMessageRegular{
				Payload:     payload,
				ConfigSeq:   configSeq,
				Class:       hb.HcsMessageRegular_NORMAL,
				OriginalSeq: originalSeq,
			},
		},
	}
}

func newTimeToCutMessageWithBlockNumber(blockNumber uint64) *hb.HcsMessage {
	return &hb.HcsMessage{
		Type: &hb.HcsMessage_TimeToCut{
			TimeToCut: &hb.HcsMessageTimeToCut{
				Request: &hb.HcsMessageTimeToCut_BlockNumber{
					BlockNumber: blockNumber,
				},
			},
		},
	}
}

func newTimeToCutMessageWithMessageHash(messageHash []byte) *hb.HcsMessage {
	return &hb.HcsMessage{
		Type: &hb.HcsMessage_TimeToCut{
			TimeToCut: &hb.HcsMessageTimeToCut{
				Request: &hb.HcsMessageTimeToCut_MessageHash{
					MessageHash: messageHash,
				},
			},
		},
	}
}

func newOrdererStartedMessage(identity []byte) *hb.HcsMessage {
	return &hb.HcsMessage{
		Type: &hb.HcsMessage_OrdererStarted{
			OrdererStarted: &hb.HcsMessageOrdererStarted{
				OrdererIdentity: identity,
			},
		},
	}
}

func newHcsMetadata(
	lastConsensusTimestampPersisted *timestamp.Timestamp,
	lastOriginalSequenceProcessed uint64,
	lastResubmittedConfigSequence uint64,
	lastChunkFreeConsensusTimestampPersisted *timestamp.Timestamp,
	lastChunkFreeSequenceProcessed uint64,
) *hb.HcsMetadata {
	return &hb.HcsMetadata{
		LastConsensusTimestampPersisted:          lastConsensusTimestampPersisted,
		LastOriginalSequenceProcessed:            lastOriginalSequenceProcessed,
		LastResubmittedConfigSequence:            lastResubmittedConfigSequence,
		LastChunkFreeConsensusTimestampPersisted: lastChunkFreeConsensusTimestampPersisted,
		LastChunkFreeSequenceProcessed:           lastChunkFreeSequenceProcessed,
	}
}

func timestampProtoOrPanic(t time.Time) *timestamp.Timestamp {
	ts, err := ptypes.TimestampProto(t)
	if err != nil {
		logger.Panicf("failed to convert time.Time to google.protobuf.Timestamp = %v", err)
		return nil
	}
	return ts
}

func isSubscriptionErrorRecoverable(code codes.Code) bool {
	recoverable := true
	switch code {
	// prior to mirror node v0.6.0, InvalidArgument is returned when a topic does not exist
	case codes.InvalidArgument:
	// as of v0.6.0, mirror node will return NotFound when a topic does not exist
	case codes.NotFound:
	// as of v0.6.0, Unavailable is returned when the connection to the database is down
	case codes.Unavailable:
	default:
		recoverable = false
	}
	return recoverable
}
