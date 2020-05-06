/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"golang.org/x/crypto/ed25519"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/hyperledger/fabric/orderer/consensus/hcs/factory"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/hashgraph/hedera-sdk-go"
	"github.com/hyperledger/fabric-protos-go/common"
	cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/orderer/consensus"
	hb "github.com/hyperledger/fabric/orderer/consensus/hcs/proto"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
)

var (
	unixEpoch               = time.Unix(0, 0)
	defaultHcsClientFactory = &hcsClientFactoryImpl{}
)

const (
	maxConsensusMessageSize    = 3500 // the max message size HCS supports is 4kB, including header
	subscriptionRetryBaseDelay = 100 * time.Millisecond
	subscriptionRetryMax       = 8
)

func getStateFromMetadata(metadataValue []byte, channelID string) (time.Time, uint64, uint64, time.Time) {
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
			lastChunkFreeTimestamp
	}

	// defaults
	return unixEpoch, 0, 0, unixEpoch
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
) (*chainImpl, error) {
	lastCutBlockNumber := support.Height() - 1
	logger.Infof("[channel: %s] starting chain with last persisted consensus timestamp %d and "+
		"last recorded block [%d]", support.ChannelID(), lastConsensusTimestampPersisted.UnixNano(), lastCutBlockNumber)

	topicID, publicKeys, network, operatorID, operatorPrivateKey, err := parseConfig(support.SharedConfig().ConsensusMetadata(), consenter.sharedHcsConfig())
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
	var gcmCipher cipher.AEAD
	if consenter.sharedHcsConfig().EncryptionKey != "" {
		if gcmCipher, err = makeGCMCipher(consenter.sharedHcsConfig().EncryptionKey); err != nil {
			logger.Errorf("[channel: %s] failed to create gcm cipher = %v", support.ChannelID(), err)
			return nil, err
		}
	}

	consenter.Metrics().NumberNodes.With("channel", support.ChannelID()).Set(float64(len(support.ChannelConfig().OrdererAddresses())))
	consenter.Metrics().CommittedBlockNumber.With("channel", support.ChannelID()).Set(float64(lastCutBlockNumber))
	consenter.Metrics().LastConsensusTimestampPersisted.With("channel", support.ChannelID()).Set(float64(lastConsensusTimestampPersisted.UnixNano()))

	chain := &chainImpl{
		consenter:                       consenter,
		ConsenterSupport:                support,
		hcf:                             hcf,
		lastConsensusTimestampPersisted: lastConsensusTimestampPersisted,
		lastConsensusTimestamp:          lastConsensusTimestampPersisted,
		lastOriginalSequenceProcessed:   lastOriginalSequenceProcessed,
		lastResubmittedConfigSequence:   lastResubmittedConfigSequence,
		lastChunkFreeConsensusTimestamp: lastChunkFreeConsensusTimestamp,
		lastCutBlockNumber:              lastCutBlockNumber,
		network:                         network,
		operatorID:                      operatorID,
		operatorPrivateKey:              operatorPrivateKey,
		publicKeys:                      publicKeys,
		topicID:                         &topicID,
		haltChan:                        make(chan struct{}),
		startChan:                       make(chan struct{}),
		doneReprocessingMsgInFlight:     doneReprocessingMsgInFlight,
		maxChunkAge:                     calcMaxChunkAge(200, len(support.ChannelConfig().OrdererAddresses())),
		gcmCipher:                       gcmCipher,
		nonceReader:                     crand.Reader,
		appID:                           appID[:],
	}

	var blkCipher blockCipher
	if gcmCipher != nil {
		blkCipher = chain
	}
	if chain.appMsgProcessor, err = newAppMsgProcessor(chain.appID, maxConsensusMessageSize, chain, blkCipher); err != nil {
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
	lastConsensusTimestamp          time.Time
	lastOriginalSequenceProcessed   uint64
	lastResubmittedConfigSequence   uint64
	lastChunkFreeConsensusTimestamp time.Time
	lastCutBlockNumber              uint64

	// mutex used when changing the doneReprocessingMsgInFlight
	doneReprocessingMutex sync.Mutex
	// notification that there are in-flight messages need to wait for
	doneReprocessingMsgInFlight chan struct{}

	// when the topic consumer errors, close the channel. Otherwise, make this
	// an open, unbuffered channel
	errorChan chan struct{}
	// when a Halt() request comes, close the channel.
	haltChan               chan struct{}
	doneProcessingMessages chan struct{}
	startChan              chan struct{}
	// timer control the batch timeout of cutting pending messages into a block
	timer <-chan time.Time

	network                 map[string]hedera.AccountID
	operatorID              *hedera.AccountID
	operatorPrivateKey      *hedera.Ed25519PrivateKey
	publicKeys              []*hedera.Ed25519PublicKey
	topicID                 *hedera.ConsensusTopicID
	topicProducer           factory.ConsensusClient
	topicConsumer           factory.MirrorClient
	topicSubscriptionHandle factory.MirrorSubscriptionHandle
	subscriptionRetryTimer  <-chan time.Time

	appMsgProcessor appMsgProcessor
	appID           []byte
	maxChunkAge     uint64

	gcmCipher   cipher.AEAD
	nonceReader io.Reader
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
	select {
	case <-chain.startChan:
		return chain.errorChan
	default:
		dummyError := make(chan struct{})
		close(dummyError)
		return dummyError
	}
}

// signer interface
func (chain *chainImpl) Sign(message []byte) ([]byte, error) {
	if message == nil || len(message) == 0 {
		return nil, fmt.Errorf("invalid data")
	}
	return chain.operatorPrivateKey.Sign(message), nil
}

func (chain *chainImpl) Verify(message, signature []byte) bool {
	for _, publicKey := range chain.publicKeys {
		if ed25519.Verify(publicKey.Bytes(), message, signature) {
			return true
		}
	}
	return false
}

func (chain *chainImpl) Encrypt(plaintext []byte) (iv []byte, ciphertext []byte, err error) {
	if chain.gcmCipher == nil {
		logger.Errorf("[channel: %s] Encrypt is called with nil gcmCipher", chain.ChannelID())
		return nil, nil, fmt.Errorf("cipher is not initialized")
	}

	iv = make([]byte, chain.gcmCipher.NonceSize())
	if _, err := io.ReadFull(chain.nonceReader, iv); err != nil {
		logger.Errorf("[channel: %s] failed to read nonce from secure random source - err", err)
		return nil, nil, err
	}

	ciphertext = chain.gcmCipher.Seal(nil, iv, plaintext, nil)
	return iv, ciphertext, nil
}

func (chain *chainImpl) Decrypt(iv []byte, ciphertext []byte) (plaintext []byte, err error) {
	if chain.gcmCipher == nil {
		logger.Errorf("[channel: %s] Encrypt is called with nil gcmCipher", chain.ChannelID())
		return nil, fmt.Errorf("cipher is not initialized")
	}

	if plaintext, err = chain.gcmCipher.Open(nil, iv, ciphertext, nil); err != nil {
		logger.Errorf("[channel: %s] failed to decrypt ciphertext - %v", chain.ChannelID(), err)
		return nil, err
	}
	return
}

func (chain *chainImpl) processMessages() error {
	defer func() {
		chain.topicSubscriptionHandle.Unsubscribe()
		if err := chain.topicConsumer.Close(); err != nil {
			logger.Errorf("[channel: %s] error when closing topicConsumer = %v", chain.ChannelID(), err)
		}
		if err := chain.topicProducer.Close(); err != nil {
			logger.Errorf("[channel: %s] error when closing topicProducer = %v", chain.ChannelID(), err)
		}
		close(chain.doneProcessingMessages)

		select {
		case <-chain.errorChan: // If already closed, don't do anything
		default:
			close(chain.errorChan)
		}
	}()

	recollectPendingChunks := !chain.lastConsensusTimestampPersisted.Equal(chain.lastChunkFreeConsensusTimestamp)
	if recollectPendingChunks {
		// recollect chunks pending reassembly at the time of last shutdown / crash, handle it by adding all messages
		// in the range (lastChunkFreeBlock.LastConsensusTimestampPersisted, chain.LastConsensusTimestampPersisted] to appMsgProcessor
		logger.Debugf("[channel: %s] going to collect chunks pending reassembly at the time of last shutdown / crash", chain.ChannelID())
	} else {
		logger.Debugf("[channel: %s] going into the normal message processing loop", chain.ChannelID())
	}
	subscriptionRetryCount := 0
	msg := new(hb.HcsMessage)
	for {
		select {
		case <-chain.haltChan:
			logger.Warningf("[channel: %s] consenter for channel exiting", chain.ChannelID())
			return nil
		case hcsErr := <-chain.topicSubscriptionHandle.Errors():
			logger.Errorf("[channel: %s] error received during subscription streaming, %v", chain.ChannelID(), hcsErr)
			select {
			case <-chain.errorChan: // don't do anything if already closed
			default:
				logger.Errorf("[channel: %s] closing errorChan due to subscription streaming error", chain.ChannelID())
				close(chain.errorChan)
			}
			st, ok := status.FromError(hcsErr)
			if ok && isSubscriptionErrorRecoverable(st.Code()) && subscriptionRetryCount < subscriptionRetryMax {
				// the new topic may have not propagated to the mirror node yet, retry subscription
				delay := time.Duration(float64(subscriptionRetryBaseDelay) * math.Pow(2, float64(subscriptionRetryCount)))
				logger.Infof("[channel: %s] the topic may be not ready yet, retry in %dms", chain.ChannelID(), delay.Milliseconds())
				chain.subscriptionRetryTimer = time.After(delay)
			} else {
				logger.Errorf("[channel: %s] closing haltChan due to subscription streaming error", chain.ChannelID())
				close(chain.haltChan)
			}
		case <-chain.subscriptionRetryTimer:
			logger.Debugf("[channel: %s] retry topic subscription", chain.ChannelID())
			chain.topicSubscriptionHandle.Unsubscribe()
			chain.subscriptionRetryTimer = nil
			subscriptionRetryCount++
			if err := startSubscription(chain, chain.lastConsensusTimestamp); err != nil {
				logger.Errorf("[channel: %s] closing haltChan due to failed subscription retry = %v", chain.ChannelID(), err)
				close(chain.haltChan)
			}
		case resp, ok := <-chain.topicSubscriptionHandle.Responses():
			if !ok {
				logger.Criticalf("[channel: %s] hcs topic subscription closed", chain.ChannelID())
				return nil
			}
			subscriptionRetryCount = 0
			select {
			case <-chain.errorChan:
				chain.errorChan = make(chan struct{}) // make a new one, make the chain available again
				logger.Infof("[channel: %s] marked chain as available again", chain.ChannelID())
			default:
			}

			chunk := new(hb.ApplicationMessageChunk)
			if err := proto.Unmarshal(resp.Message, chunk); err != nil {
				logger.Errorf("[channel: %s] failed to unmarshal ordered message into ApplicationMessageChunk = %v", chain.ChannelID(), err)
				continue
			}
			payload, err := chain.appMsgProcessor.Reassemble(chunk)
			count := chain.appMsgProcessor.ExpireByAge(chain.maxChunkAge)
			chain.consenter.Metrics().NumberMessagesDropped.With("channel", chain.ChannelID()).Add(float64(count))
			if err != nil {
				logger.Errorf("[channel: %s] failed to process a received chunk - %v", chain.ChannelID(), err)
				continue
			}
			if payload == nil {
				logger.Debugf("need more chunks to reassemble the HCS message")
				continue
			}
			logger.Debugf("[channel: %s] reassembled a message of %d bytes", chain.ChannelID(), len(payload))
			if err := proto.Unmarshal(payload, msg); err != nil {
				logger.Criticalf("[channel: %s] unable to unmarshal ordered message", chain.ChannelID())
				continue
			}
			logger.Debugf("[channel %s] successfully unmarshaled ordered message, consensus timestamp %d",
				chain.ChannelID(), resp.ConsensusTimeStamp.Nanosecond())
			if !recollectPendingChunks {
				// use ConseusTimestamp and SequenceNumber of the last received chunk for that of a message
				switch msg.Type.(type) {
				case *hb.HcsMessage_Regular:
					if err := chain.processRegularMessage(msg.GetRegular(), resp.ConsensusTimeStamp, resp.SequenceNumber); err != nil {
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
				if resp.ConsensusTimeStamp.Equal(chain.lastConsensusTimestamp) {
					recollectPendingChunks = false
					logger.Debugf("[channel: %s] switching to the normal message processing loop", chain.ChannelID())
					if chain.lastResubmittedConfigSequence == 0 || chain.lastResubmittedConfigSequence <= chain.lastOriginalSequenceProcessed {
						// unblock ingress
						logger.Debugf("[channel: %s] unblock ingress", chain.ChannelID())
						chain.reprocessComplete()
					}
				} else if resp.ConsensusTimeStamp.After(chain.lastConsensusTimestamp) {
					logger.Panicf("[channel: %s] consensus timestamp (%d) of last processed message is later "+
						"than chain.lastConsensusTimestampPersisted (%d)", chain.ChannelID(), resp.ConsensusTimeStamp.UnixNano(),
						chain.lastConsensusTimestampPersisted.UnixNano())
				}
			}
		case <-chain.timer:
			if err := chain.sendTimeToCut(); err != nil {
				logger.Errorf("[channel: %s] cannot send time-to-cut message, %s", chain.ChannelID(), err)
			}
		}
	}
}

func (chain *chainImpl) WriteBlock(block *cb.Block, isConfig bool, consensusTimestamp time.Time) {
	chain.lastCutBlockNumber++
	if !chain.appMsgProcessor.IsPending() {
		chain.lastChunkFreeConsensusTimestamp = consensusTimestamp
	}
	metadata := newHcsMetadata(
		timestampProtoOrPanic(consensusTimestamp),
		chain.lastOriginalSequenceProcessed,
		chain.lastResubmittedConfigSequence,
		timestampProtoOrPanic(chain.lastChunkFreeConsensusTimestamp))
	marshaledMetadata := protoutil.MarshalOrPanic(metadata)
	if !isConfig {
		chain.ConsenterSupport.WriteBlock(block, marshaledMetadata)
	} else {
		chain.ConsenterSupport.WriteConfigBlock(block, marshaledMetadata)
	}
	chain.consenter.Metrics().CommittedBlockNumber.With("channel", chain.ChannelID()).Set(float64(chain.lastCutBlockNumber))
	chain.consenter.Metrics().LastConsensusTimestampPersisted.With("channel", chain.ChannelID()).Set(float64(consensusTimestamp.UnixNano()))
}

func (chain *chainImpl) commitNormalMessage(message *cb.Envelope, curConsensusTimestamp time.Time, newOriginalSequenceProcessed uint64) {
	batches, pending := chain.BlockCutter().Ordered(message)
	logger.Debugf("[channel: %s] Ordering results: items in batch = %d, pending = %v", chain.ChannelID(), len(batches), pending)

	switch {
	case chain.timer != nil && !pending:
		// Timer is already running but there are no messages pending, stop the timer
		chain.timer = nil
	case chain.timer == nil && pending:
		// Timer is not already running and there are messages pending, so start it
		chain.timer = time.After(chain.SharedConfig().BatchTimeout())
		logger.Debugf("[channel: %s] Just began %s batch timer", chain.ChannelID(), chain.SharedConfig().BatchTimeout().String())
	default:
		// Do nothing when:
		// 1. Timer is already running and there are messages pending
		// 2. Timer is not set and there are no messages pending
	}

	if len(batches) == 0 {
		// If no block is cut, we update the `lastOriginalSequenceProcessed`, start the timer if necessary and return
		chain.lastOriginalSequenceProcessed = newOriginalSequenceProcessed
		chain.lastConsensusTimestamp = curConsensusTimestamp
		return
	}

	if pending || len(batches) == 2 {
		// If the newest envelope is not encapsulated into the first batch,
		// the `LastConsensusTimestampPersisted` should be `chain.lastConsensusTimestamp`
		// the 'LastOriginalSequenceProcessed` should be `chain.lastOriginalSequenceProcessed`
	} else {
		// We are just cutting exactly one block, so it is safe to update
		// `lastOriginalSequenceProcessed` with `newOriginalSequenceProcessed` here, and then
		// encapsulate it into this block. Otherwise, if we are cutting two
		// blocks, the first one should use current `lastOriginalSequenceProcessed`
		// and the second one should use `newOriginalSequenceProcessed`, which is also used to
		// update `lastOriginalSequenceProcessed`
		chain.lastOriginalSequenceProcessed = newOriginalSequenceProcessed
		chain.lastConsensusTimestamp = curConsensusTimestamp
	}

	// Commit the first block
	block := chain.CreateNextBlock(batches[0])
	chain.WriteBlock(block, false, chain.lastConsensusTimestamp)
	logger.Debugf("[channel: %s] Batch filled, just cut block [%d] - last persisted consensus timestamp"+
		" is now %d", chain.ChannelID(), chain.lastCutBlockNumber, chain.lastConsensusTimestamp.UnixNano())

	// Commit the second block if exists
	if len(batches) == 2 {
		chain.lastOriginalSequenceProcessed = newOriginalSequenceProcessed
		chain.lastConsensusTimestamp = curConsensusTimestamp

		block := chain.CreateNextBlock(batches[1])
		chain.WriteBlock(block, false, curConsensusTimestamp)
		logger.Debugf("[channel: %s] Batch filled, just cut block [%d] - last persisted consensus "+
			"timestamp is now %d", chain.ChannelID(), chain.lastCutBlockNumber, curConsensusTimestamp.UnixNano())
	}
}

func (chain *chainImpl) commitConfigMessage(message *cb.Envelope, curConsensusTimestamp time.Time, newOriginalSequenceProcessed uint64) {
	logger.Debugf("[channel: %s] Received config message", chain.ChannelID())
	batch := chain.BlockCutter().Cut()

	if batch != nil {
		logger.Debugf("[channel: %s] Cut pending messages into block", chain.ChannelID())
		block := chain.CreateNextBlock(batch)
		chain.WriteBlock(block, false, chain.lastConsensusTimestamp)
	}

	logger.Debugf("[channel: %s] Creating isolated block for config message", chain.ChannelID())
	chain.lastOriginalSequenceProcessed = newOriginalSequenceProcessed
	chain.lastConsensusTimestamp = curConsensusTimestamp
	block := chain.CreateNextBlock([]*cb.Envelope{message})
	chain.WriteBlock(block, true, curConsensusTimestamp)
	chain.timer = nil
}

func (chain *chainImpl) processRegularMessage(msg *hb.HcsMessageRegular, ts time.Time, receivedSequence uint64) error {
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

		chain.commitNormalMessage(env, ts, newSeq)

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

		chain.commitConfigMessage(env, ts, newSeq)
		// new configuration is applied, number of orderers may have changed
		chain.consenter.Metrics().NumberNodes.With("channel", chain.ChannelID()).Set(float64(len(chain.ChannelConfig().OrdererAddresses())))
		chain.maxChunkAge = calcMaxChunkAge(200, len(chain.ChannelConfig().OrdererAddresses()))
		logger.Debugf("[channel: %s] channel configuration has changed, updated maxChunkAge to %d", chain.ChannelID(), chain.maxChunkAge)

		// update publicKeys
		hcsConfigMetadata := &hb.HcsConfigMetadata{}
		if err := proto.Unmarshal(chain.SharedConfig().ConsensusMetadata(), hcsConfigMetadata); err != nil {
			logger.Panicf("[channel: %s] invalid consensus metadata in new channel configuration - %v", chain.ChannelID(), err)
		}
		publicKeys := make([]*hedera.Ed25519PublicKey, len(hcsConfigMetadata.PublicKeys))
		for index, keyStr := range hcsConfigMetadata.PublicKeys {
			publicKey, err := hedera.Ed25519PublicKeyFromString(keyStr)
			if err != nil {
				logger.Panicf("[channel: %s] public key at %d is invalid in configuration metadata - %v", chain.ChannelID(), index, err)
			}
			publicKeys[index] = &publicKey
		}
		chain.publicKeys = publicKeys
		logger.Debug("[channel: %s] channel configuration has changed, publicKeys gets re-parsed", chain.ChannelID())
	default:
		return errors.Errorf("unsupported regular HCS message type: %v", msg.Class.String())
	}

	return nil
}

func (chain *chainImpl) processTimeToCutMessage(msg *hb.HcsMessageTimeToCut, ts time.Time, sequence uint64) error {
	blockNumber := msg.GetBlockNumber()
	if blockNumber == chain.lastCutBlockNumber+1 {
		chain.timer = nil
		batch := chain.BlockCutter().Cut()
		if len(batch) == 0 {
			return fmt.Errorf("bug, got correct time-to-cut message (block %d), "+
				"but no pending transactions", blockNumber)
		}
		block := chain.CreateNextBlock(batch)
		chain.lastConsensusTimestamp = ts
		chain.lastOriginalSequenceProcessed = sequence
		chain.WriteBlock(block, false, ts)
		logger.Debugf("[channel: %s] successfully cut block %d, triggered by time-to-cut",
			chain.ChannelID(), blockNumber)
	} else if blockNumber > chain.lastCutBlockNumber+1 {
		return fmt.Errorf("discard larger time-to-cut message (%d) than expected (%d)",
			blockNumber, chain.lastCutBlockNumber+1)
	}
	logger.Debugf("[channel: %s] ignore stale/late time-to-cut (block %d)", chain.ChannelID(), blockNumber)
	return nil
}

func (chain *chainImpl) processOrdererStartedMessage(msg *hb.HcsMessageOrdererStarted) {
	logger.Debugf("[channel: %s] orderer %s just started", chain.ChannelID(), hex.EncodeToString(msg.OrdererIdentity))
	if count, err := chain.appMsgProcessor.ExpireByAppID(msg.OrdererIdentity); err == nil {
		logger.Debugf("[channel: %s] %d pending messages from orderer %s dropped", chain.ChannelID(), count, hex.EncodeToString(msg.OrdererIdentity))
		chain.consenter.Metrics().NumberMessagesDropped.With("channel", chain.ChannelID()).Add(float64(count))
	} else {
		logger.Errorf("[channel: %s] ExpireByAppID returns error = %v", chain.ChannelID(), err)
	}
}

func (chain *chainImpl) sendTimeToCut() error {
	chain.timer = nil
	msg := newTimeToCutMessage(chain.lastCutBlockNumber + 1)
	if !chain.enqueue(msg, false) {
		return errors.Errorf("[channel: %s] failed to send time-to-cut with block number %d",
			chain.ChannelID(), chain.lastCutBlockNumber+1)
	}
	logger.Infof("[channel: %s] time to cut with block number %d sent to topic %v",
		chain.ChannelID(), chain.lastCutBlockNumber+1, chain.topicID)
	return nil
}

func (chain *chainImpl) order(env *cb.Envelope, configSeq uint64, originalOffset uint64) error {
	marshaledEnv, err := protoutil.Marshal(env)
	if err != nil {
		return errors.Errorf("cannot enqueue, unable to marshal envelope: %s", err)
	}
	if !chain.enqueue(newNormalMessage(marshaledEnv, configSeq, originalOffset), originalOffset != 0) {
		return errors.Errorf("cannot enqueue")
	}
	return nil
}

func makeGCMCipher(keyStr string) (cipher.AEAD, error) {
	if len(keyStr) != 32 {
		return nil, fmt.Errorf("failed to create the cipher, key size is not 256 bit")
	}

	block, err := aes.NewCipher([]byte(keyStr))
	if err != nil {
		return nil, fmt.Errorf("failed to create the AES block cipher, err = %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create the GCM cipher, err = %v", err)
	}
	return gcm, nil
}

func (chain *chainImpl) configure(config *cb.Envelope, configSeq uint64, originalOffset uint64) error {
	marshaledConfig, err := protoutil.Marshal(config)
	if err != nil {
		return errors.Errorf("unable to marshal config because %s", err)
	}
	if !chain.enqueue(newConfigMessage(marshaledConfig, configSeq, originalOffset), originalOffset != 0) {
		return fmt.Errorf("cannot enqueue the config message")
	}
	return nil
}

func (chain *chainImpl) enqueue(message *hb.HcsMessage, isResubmission bool) bool {
	logger.Debugf("[channel: %s] Enqueueing envelope...", chain.ChannelID())
	select {
	case <-chain.startChan: // The Start phase has completed
		select {
		case <-chain.haltChan: // The chain has been halted, stop here
			logger.Warningf("[channel: %s] consenter for this channel has been halted", chain.ChannelID())
			return false
		default: // The post path
			return chain.enqueueChecked(message, isResubmission)
		}
	default: // Not ready yet
		logger.Warningf("[channel: %s] Will not enqueue, consenter for this channel hasn't started yet", chain.ChannelID())
		return false
	}
}

func (chain *chainImpl) enqueueChecked(message *hb.HcsMessage, isResubmission bool) bool {
	payload, err := protoutil.Marshal(message)
	if err != nil {
		logger.Errorf("[channel: %s] unable to marshal HCS message because = %s", chain.ChannelID(), err)
		return false
	}
	chunks, err := chain.appMsgProcessor.Split(payload)
	if err != nil {
		logger.Errorf("[channel: %s] failed to split message - %v", chain.ChannelID(), err)
		return false
	}
	logger.Debugf("[channel: %s] the payload of %d bytes is cut into %d chunks, resubmission ? %v",
		chain.ChannelID(), len(payload), len(chunks), isResubmission)
	for _, chunk := range chunks {
		if _, err := chain.topicProducer.SubmitConsensusMessage(protoutil.MarshalOrPanic(chunk), chain.topicID); err != nil {
			logger.Errorf("[channel: %s] cannot enqueue envelope because = %s", chain.ChannelID(), err)
			return false
		}
		logger.Debugf("[channel: %s] chunk %d of message sent successfully", chain.ChannelID(), chunk.ChunkIndex)
	}
	logger.Debugf("[channel: %s] Envelope enqueued successfully", chain.ChannelID())
	return true
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
	err = startSubscription(chain, chain.lastChunkFreeConsensusTimestamp)
	if err != nil {
		logger.Panicf("[channel: %s] failed to start topic subscription = %v", chain.ChannelID(), err)
	}

	chain.doneProcessingMessages = make(chan struct{})
	chain.errorChan = make(chan struct{})

	if !chain.enqueueChecked(newOrdererStartedMessage(chain.appID), false) {
		logger.Panicf("[channel: %s] failed to send orderer started message", chain.ChannelID())
	}

	close(chain.startChan)
	logger.Infof("[channel: %s] Start phase completed successfully", chain.ChannelID())

	chain.processMessages()
}

func parseConfig(
	configMetaData []byte,
	config *localconfig.Hcs,
) (topicID hedera.ConsensusTopicID, publicKeys []*hedera.Ed25519PublicKey, network map[string]hedera.AccountID, operatorID *hedera.AccountID, privateKey *hedera.Ed25519PrivateKey, err error) {
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

	publicKey := tmpPrivateKey.PublicKey()
	tmpPublicKeys := []*hedera.Ed25519PublicKey{&publicKey}
	for index, keyStr := range hcsConfigMetadata.PublicKeys {
		if keyStr == tmpPublicKeys[0].String() {
			continue
		}
		publicKey, err = hedera.Ed25519PublicKeyFromString(keyStr)
		if err != nil {
			err = fmt.Errorf("public key %d in config metadata is invalid - %v", index, err)
			return
		}
		tmpPublicKeys = append(tmpPublicKeys, &publicKey)
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

func newTimeToCutMessage(blockNumber uint64) *hb.HcsMessage {
	return &hb.HcsMessage{
		Type: &hb.HcsMessage_TimeToCut{
			TimeToCut: &hb.HcsMessageTimeToCut{
				BlockNumber: blockNumber,
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
) *hb.HcsMetadata {
	return &hb.HcsMetadata{
		LastConsensusTimestampPersisted:          lastConsensusTimestampPersisted,
		LastOriginalSequenceProcessed:            lastOriginalSequenceProcessed,
		LastResubmittedConfigSequence:            lastResubmittedConfigSequence,
		LastChunkFreeConsensusTimestampPersisted: lastChunkFreeConsensusTimestampPersisted,
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

func calcMaxChunkAge(maxAgeBase uint64, numOrderers int) uint64 {
	return maxAgeBase * uint64(numOrderers)
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
