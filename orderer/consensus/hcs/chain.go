/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/hashgraph/hedera-sdk-go"
	"github.com/hyperledger/fabric-protos-go/common"
	cb "github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.com/hyperledger/fabric/orderer/consensus"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
)

var unixEpoch = time.Unix(0, 0)

func getStateFromMetadata(metadataValue []byte, chainID string) (time.Time, uint64, uint64, uint64) {
	if metadataValue != nil {
		hcsMetadata := &ab.HcsMetadata{}
		if err := proto.Unmarshal(metadataValue, hcsMetadata); err != nil {
			logger.Panicf("[channel: %s] Ledger may be corrupted: "+
				"cannot unmarshal orderer metadata in most recent block", chainID)
		}
		timestamp, err := ptypes.Timestamp(hcsMetadata.GetLastConsensusTimestampPersisted())
		if err != nil {
			logger.Panicf("[channel: %s] Ledger may be corrupted: "+
				"cannot unmarshal orderer metadata in most recent block, %v", chainID, err)
		}
		return timestamp,
			hcsMetadata.GetLastOriginalSequenceProcessed(),
			hcsMetadata.GetLastResubmittedConfigSequence(),
			hcsMetadata.GetLastFragmentId()
	}

	// defaults
	return unixEpoch, 0, 0, 0
}

func makeRandomKey(prefix string) string {
	data := make([]byte, 128)
	rand.Seed(time.Now().UnixNano())
	rand.Read(data)
	data = append(data, []byte(prefix)...)
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])[0:16]
}

func newChain(
	consenter commonConsenter,
	support consensus.ConsenterSupport,
	lastConsensusTimestampPersisted time.Time,
	lastOriginalSequenceProcessed uint64,
	lastResubmittedConfigSequence uint64,
	lastFragmentId uint64,
) (*chainImpl, error) {
	lastCutBlockNumber := support.Height() - 1
	logger.Infof("[channel: %s] starting chain with last persisted consensus timestamp %d and " +
		"last recorded block [%d]", support.ChannelID(), lastConsensusTimestampPersisted.UnixNano(), lastCutBlockNumber)

	doneReprocessingMsgInFlight := make(chan struct{})
	// In either one of following cases, we should unblock ingress messages:
	// - lastResubmittedConfigOffset == 0, where we've never resubmitted any config messages
	// - lastResubmittedConfigOffset == lastOriginalOffsetProcessed, where the latest config message we resubmitted
	//   has been processed already
	// - lastResubmittedConfigOffset < lastOriginalOffsetProcessed, where we've processed one or more resubmitted
	//   normal messages after the latest resubmitted config message. (we advance `lastResubmittedConfigOffset` for
	//   config messages, but not normal messages)
	if lastResubmittedConfigSequence == 0 || lastResubmittedConfigSequence <= lastOriginalSequenceProcessed {
		// If we've already caught up with the reprocessing resubmitted messages, close the channel to unblock broadcast
		close(doneReprocessingMsgInFlight)
	}

	return &chainImpl{
		consenter:                       consenter,
		ConsenterSupport:                support,
		lastConsensusTimestampPersisted: lastConsensusTimestampPersisted,
		lastConsensusTimestamp:          lastConsensusTimestampPersisted,
		lastOriginalSequenceProcessed:   lastOriginalSequenceProcessed,
		lastResubmittedConfigSequence:   lastResubmittedConfigSequence,
		lastFragmentId:                  lastFragmentId,
		lastCutBlockNumber:              lastCutBlockNumber,
		haltChan:                        make(chan struct{}),
		startChan:                       make(chan struct{}),
		doneReprocessingMsgInFlight:     doneReprocessingMsgInFlight,
		network:                         make(map[string]hedera.AccountID, len(consenter.sharedHcsConfig().Nodes)),
		consensusRespChan:               make(chan *hedera.MirrorConsensusTopicResponse),
		consensusErrorChan:              make(chan error),
		fragmenter:                      newFragmentSupport(),
		fragmentKey:                     makeRandomKey(consenter.sharedHcsConfig().Operator.Id),
	}, nil
}

type chainImpl struct {
	consenter commonConsenter
	consensus.ConsenterSupport

	lastConsensusTimestampPersisted time.Time
	lastConsensusTimestamp          time.Time
	lastOriginalSequenceProcessed   uint64
	lastResubmittedConfigSequence   uint64
	lastFragmentId                  uint64
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
	topicId                 hedera.ConsensusTopicID
	topicProducer           *hedera.Client
	singleNodeTopicProducer *hedera.Client
	topicConsumer           *hedera.MirrorClient
	topicSubscriptionHandle *hedera.MirrorSubscriptionHandle

	consensusRespChan  chan *hedera.MirrorConsensusTopicResponse
	consensusErrorChan chan error

	fragmenter  *fragmentSupport
	fragmentKey string
}

func (chain *chainImpl) Order(env *common.Envelope, configSeq uint64) error {
	return chain.order(env, configSeq, 0)
}

func (chain *chainImpl) Configure(config *common.Envelope, configSeq uint64) error {
	return chain.configure(config, configSeq, 0)
}

func (chain *chainImpl) WaitReady() error {
	select {
	case <-chain.startChan:
		select {
		case <-chain.haltChan:
			return fmt.Errorf("consenter for this channel has been halted")
		case <-chain.doneReprocessing(): // Block waiting for all re-submitted messages to be reprocessed
			return nil
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

func startThread(chain *chainImpl) {
	var err error

	// create topicProducer
	chain.topicProducer, chain.singleNodeTopicProducer, err = setupTopicProducer(chain.consenter.sharedHcsConfig(), chain.network)
	if err != nil {
		logger.Panicf("[channel: %s] failed to set up topic producer, %s", chain.ChannelID(), err)
	}

	// create topicConsumer and start subscription
	chain.topicConsumer, err = setupTopicConsumer(chain.consenter.sharedHcsConfig().MirrorNodeAddress)
	if err != nil {
		logger.Panicf("[channel: %s] failed to set up topic consumer, %s", chain.ChannelID(), err)
	}
	logger.Debugf("[channel: %s] created topic consumer with mirror node address %s",
		chain.ChannelID(), chain.consenter.sharedHcsConfig().MirrorNodeAddress)

	// subscribe to the hcs topic
	// hack, use channel id as the topic id, SKIP the first character, it has to be a letter to pass
	// validation
	chain.topicId, err = hedera.TopicIDFromString(chain.ChannelID()[1:])
	if err != nil {
		logger.Panicf("[channel: %s] invalid HCS topic ID %s", chain.ChannelID(), chain.ChannelID()[1:])
	}
	startTime := chain.lastConsensusTimestampPersisted
	if !startTime.Equal(unixEpoch) {
		startTime = startTime.Add(time.Nanosecond)
	}
	handle, err := hedera.NewMirrorConsensusTopicQuery().
		SetTopicID(chain.topicId).
		SetStartTime(startTime).
		Subscribe(
			*chain.topicConsumer,
			func(resp hedera.MirrorConsensusTopicResponse) {
				logger.Debugf("[channel: %s] received consensus response, sequence = %d", chain.ChannelID(), resp.SequenceNumber)
				chain.consensusRespChan <- &resp
			},
			func(err error) {
				logger.Debugf("[channel: %s] received error when handling consensus subscription, %v", chain.ChannelID(), err)
				chain.consensusErrorChan <- err
				close(chain.consensusRespChan)
			})
	if err != nil {
		logger.Panicf("[channel: %s] failed to subscribe to hcs topic %v", chain.ChannelID(),
			chain.topicId)
	}
	chain.topicSubscriptionHandle = &handle

	chain.doneProcessingMessages = make(chan struct{})

	chain.errorChan = make(chan struct{})
	close(chain.startChan)

	logger.Infof("[channel: %s] Start phase completed successfully", chain.ChannelID())

	chain.processMessages()
}

func (chain *chainImpl) processMessages() error {
	defer func() {
		chain.topicSubscriptionHandle.Unsubscribe()
		close(chain.doneProcessingMessages)
	}()

	defer func() {
		select {
		case <-chain.errorChan: // If already closed, don't do anything
		default:
			close(chain.errorChan)
		}
	}()

	msg := new(ab.HcsMessage)
	logger.Debugf("[channel: %s] going into the processMessages loop", chain.ChannelID())
	for {
		select {
		case <-chain.haltChan:
			logger.Warningf("[channel: %s] consenter for channel exiting", chain.ChannelID())
			return nil
		case hcsErr := <-chain.consensusErrorChan:
			logger.Errorf("[channel: %s] error received during subscription streaming, %s", chain.ChannelID(), hcsErr)
			select {
			case <-chain.errorChan: // don't do anything if already closed
			default:
				logger.Errorf("[channel: %s] closing errorChan due to subscription streaming error", chain.ChannelID())
				close(chain.errorChan)
			}
		case resp, ok := <-chain.consensusRespChan:
			if !ok {
				logger.Criticalf("[channel: %s] hcs topic subscription closed", chain.ChannelID())
				return nil
			}
			select {
			case <-chain.errorChan:
				chain.errorChan = make(chan struct{}) // make a new one, make the chain available again
				logger.Infof("[channel: %s] marked chain as available again", chain.ChannelID())
			default:
			}

			fragment := new(ab.HcsMessageFragment)
			if err := proto.Unmarshal(resp.Message, fragment); err != nil {
				logger.Panicf("[channel: %s] failed to unmarshal ordered message into HCS fragment", chain.ChannelID())
			}
			encrypted := chain.fragmenter.reassemble(fragment)
			if encrypted == nil {
				logger.Debugf("need more fragments to reassemble the HCS message")
				continue
			}
			logger.Debugf("[channel: %s] reassembled a message of %d bytes", chain.ChannelID(), len(encrypted))
			plain, err := chain.decrypt(encrypted)
			if err != nil {
				logger.Errorf("failed to decrypt message payload in MirrorConsensusTopicResponse")
				continue
			}
			if err := proto.Unmarshal(plain, msg); err != nil {
				logger.Critical("[channel: %s] unable to unmarshal ordered message", chain.ChannelID())
				continue
			} else {
				logger.Debugf("[channel %s] successfully unmarshalled ordered message, " +
					"consensus timestamp %v", chain.ChannelID(), resp.ConsensusTimeStamp)
			}
			switch msg.Type.(type) {
			case *ab.HcsMessage_Regular:
				if err := chain.processRegularMessage(msg.GetRegular(), &resp.ConsensusTimeStamp, resp.SequenceNumber); err != nil {
					logger.Warningf("[channel: %s] error when processing incoming message of type REGULAR = %s", chain.ChannelID(), err)
				}
			case *ab.HcsMessage_TimeToCut:
				if err := chain.processTimeToCutMessage(msg.GetTimeToCut(), &resp.ConsensusTimeStamp, resp.SequenceNumber); err != nil {
					logger.Critical("[channel: %s] consenter for channel exiting, %s", chain.ChannelID(), err)
					return err
				}
			}
		case <-chain.timer:
			if err := chain.sendTimeToCut(); err != nil {
				logger.Errorf("[channel: %s] cannot send time-to-cut message, %s", chain.ChannelID(), err)
			}
		}
	}
}

func (chain *chainImpl) WriteBlock(block *cb.Block, metadata *ab.HcsMetadata) {
	chain.ConsenterSupport.WriteBlock(block, protoutil.MarshalOrPanic(metadata))
	chain.lastCutBlockNumber++
}

func (chain *chainImpl) WriteConfigBlock(block *cb.Block, metadata *ab.HcsMetadata) {
	chain.ConsenterSupport.WriteConfigBlock(block, protoutil.MarshalOrPanic(metadata))
	chain.lastCutBlockNumber++
}

func (chain *chainImpl) commitNormalMessage(message *cb.Envelope, curConsensusTimestamp *time.Time, newOriginalSequenceProcessed uint64) {
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
		chain.lastConsensusTimestamp = *curConsensusTimestamp
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
		chain.lastConsensusTimestamp = *curConsensusTimestamp
	}

	// Commit the first block
	block := chain.CreateNextBlock(batches[0])
	consensusTimestamp := timestampProtoOrPanic(curConsensusTimestamp)
	metadata := newHcsMetadata(
		consensusTimestamp,
		chain.lastOriginalSequenceProcessed,
		chain.lastResubmittedConfigSequence,
		chain.lastFragmentId)
	chain.WriteBlock(block, metadata)
	logger.Debugf("[channel: %s] Batch filled, just cut block [%d] - last persisted consensus timestamp" +
		"is now %v", chain.ChannelID(), chain.lastCutBlockNumber, *consensusTimestamp)

	// Commit the second block if exists
	if len(batches) == 2 {
		chain.lastOriginalSequenceProcessed = newOriginalSequenceProcessed
		chain.lastConsensusTimestamp = *curConsensusTimestamp

		block := chain.CreateNextBlock(batches[1])
		consensusTimestamp = timestampProtoOrPanic(curConsensusTimestamp)
		metadata := newHcsMetadata(
			consensusTimestamp,
			chain.lastOriginalSequenceProcessed,
			chain.lastResubmittedConfigSequence,
			chain.lastFragmentId)
		chain.WriteBlock(block, metadata)
		logger.Debugf("[channel: %s] Batch filled, just cut block [%d] - last persisted consensus" +
			"timestamp is now %v", chain.ChannelID(), chain.lastCutBlockNumber, *consensusTimestamp)
	}
}

func (chain *chainImpl) commitConfigMessage(message *cb.Envelope, curConsensusTimestamp *time.Time, newOriginalSequenceProcessed uint64) {
	logger.Debugf("[channel: %s] Received config message", chain.ChannelID())
	batch := chain.BlockCutter().Cut()

	if batch != nil {
		logger.Debugf("[channel: %s] Cut pending messages into block", chain.ChannelID())
		block := chain.CreateNextBlock(batch)
		consensusTimestamp := timestampProtoOrPanic(&chain.lastConsensusTimestamp)
		metadata := newHcsMetadata(
			consensusTimestamp,
			chain.lastOriginalSequenceProcessed,
			chain.lastResubmittedConfigSequence,
			chain.lastFragmentId)
		chain.WriteBlock(block, metadata)
	}

	logger.Debugf("[channel: %s] Creating isolated block for config message", chain.ChannelID())
	chain.lastOriginalSequenceProcessed = newOriginalSequenceProcessed
	chain.lastConsensusTimestamp = *curConsensusTimestamp
	block := chain.CreateNextBlock([]*cb.Envelope{message})
	consensusTimestamp := timestampProtoOrPanic(curConsensusTimestamp)
	metadata := newHcsMetadata(
		consensusTimestamp,
		chain.lastOriginalSequenceProcessed,
		chain.lastResubmittedConfigSequence,
		chain.lastFragmentId)
	chain.WriteConfigBlock(block, metadata)
	chain.timer = nil
}

func (chain *chainImpl) processRegularMessage(msg *ab.HcsMessageRegular, ts *time.Time, receivedSequence uint64) error {
	curConfigSeq := chain.Sequence()
	env := &cb.Envelope{}
	if err := proto.Unmarshal(msg.Payload, env); err != nil {
		// This shouldn't happen, it should be filtered at ingress
		return errors.Errorf("failed to unmarshal payload of regular message because = %s", err)
	}

	logger.Debugf("[channel: %s] Processing regular HCS message of type %s", chain.ChannelID(), msg.Class.String())

	switch msg.Class {
	case ab.HcsMessageRegular_NORMAL:
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

	case ab.HcsMessageRegular_CONFIG:
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
				chain.reprocessConfigComplete() // Therefore, we could finally unblock broadcast
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
			chain.reprocessConfigPending()                     // Begin blocking ingress messages

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

	default:
		return errors.Errorf("unsupported regular HCS message type: %v", msg.Class.String())
	}

	return nil
}

func (chain *chainImpl) processTimeToCutMessage(msg *ab.HcsMessageTimeToCut, ts *time.Time, sequence uint64) error {
	blockNumber := msg.GetBlockNumber()
	if blockNumber == chain.lastCutBlockNumber+1 {
		chain.timer = nil
		batch := chain.BlockCutter().Cut()
		if len(batch) == 0 {
			return fmt.Errorf("bug, got correct time-to-cut message (block %d), " +
				"but no pending transactions", blockNumber)
		}
		block := chain.CreateNextBlock(batch)
		chain.lastConsensusTimestamp = *ts
		chain.lastOriginalSequenceProcessed = sequence
		consensusTimestamp := timestampProtoOrPanic(ts)
		metadata := &ab.HcsMetadata{
			LastConsensusTimestampPersisted: consensusTimestamp,
			LastOriginalSequenceProcessed: chain.lastOriginalSequenceProcessed,
			LastResubmittedConfigSequence: chain.lastResubmittedConfigSequence,
		}
		chain.WriteBlock(block, metadata)
		logger.Debugf("[channel: %s] successfully cut block %d, triggered by time-to-cut",
			chain.ChannelID(), blockNumber)
	} else if blockNumber > chain.lastCutBlockNumber+1 {
		return fmt.Errorf("discard larger time-to-cut message (%d) than expected (%d)",
			blockNumber, chain.lastCutBlockNumber+1)
	}
	logger.Debugf("[channel: %s] ignore stale/late time-to-cut (block %d)", chain.ChannelID(), blockNumber)
	return nil
}

func (chain *chainImpl) sendTimeToCut() error {
	chain.timer = nil
	msg := &ab.HcsMessage{
		Type: &ab.HcsMessage_TimeToCut{
			TimeToCut: &ab.HcsMessageTimeToCut{
				BlockNumber: chain.lastCutBlockNumber+1,
			},
		},
	}
	if !chain.enqueue(msg, false) {
		return errors.Errorf("[channel: %s] failed to send time-to-cut with block number %d",
			chain.ChannelID(), chain.lastCutBlockNumber+1)
	} else {
		logger.Infof("[channel: %s] time to cut with block number %d sent to topic %v",
			chain.ChannelID(), chain.lastCutBlockNumber+1, chain.topicId)
	}
	return nil
}

func setupTopicProducer(sharedHcsConfig *localconfig.Hcs, network map[string]hedera.AccountID) (*hedera.Client, *hedera.Client, error) {
	var err error
	for address, id := range sharedHcsConfig.Nodes {
		if network[address], err = hedera.AccountIDFromString(id); err != nil {
			return nil, nil, err
		}
	}
	client := hedera.NewClient(network)

	operator, err := hedera.AccountIDFromString(sharedHcsConfig.Operator.Id)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid operator account id %s", sharedHcsConfig.Operator.Id)
	}

	privateKeyConfig := &sharedHcsConfig.Operator.PrivateKey
	var privateKey hedera.Ed25519PrivateKey
	if privateKeyConfig.Enabled {
		if privateKeyConfig.Type != "ed25519" {
			return nil, nil, fmt.Errorf("unsupported privatekey type %s for operator", privateKeyConfig.Type)
		}
		privateKey, err = hedera.Ed25519PrivateKeyFromString(privateKeyConfig.Key)
		if err != nil {
			return nil, nil, errors.Errorf("failed to parse %s private key string: %s", privateKeyConfig.Type, err)
		}
	}

	client.SetOperator(operator, privateKey)

	address, id := getRandomNode(network)
	singleNetwork := map[string]hedera.AccountID{address: id}
	singleNodeClient := hedera.NewClient(singleNetwork)
	client.SetOperator(operator, privateKey)

	return client, singleNodeClient, nil
}

func getRandomNode(network map[string]hedera.AccountID) (string, hedera.AccountID) {
	index := rand.Intn(len(network))
	for address, id := range network {
		if index == 0 {
			return address, id
		}
		index--
	}
	return "", hedera.AccountID{}
}

func setupTopicConsumer(mirrorNodeAddress string) (*hedera.MirrorClient, error) {
	client, err := hedera.NewMirrorClient(mirrorNodeAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to create mirror client with mirror node address %s", mirrorNodeAddress)
	}

	return &client, nil
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

func (chain *chainImpl) encrypt(plain []byte) ([]byte, error) {
	// TODO: AES encryption
	return plain, nil
}

func (chain *chainImpl) decrypt(encrypted []byte) ([]byte, error) {
	// TODO: AES decryption
	return encrypted, nil
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

func (chain *chainImpl) enqueue(message *ab.HcsMessage, isResubmission bool) bool {
	logger.Debugf("[channel: %s] Enqueueing envelope...", chain.ChannelID())
	select {
	case <-chain.startChan: // The Start phase has completed
		select {
		case <-chain.haltChan: // The chain has been halted, stop here
			logger.Warningf("[channel: %s] consenter for this channel has been halted", chain.ChannelID())
			return false
		default: // The post path
			payload, err := protoutil.Marshal(message)
			if err != nil {
				logger.Errorf("[channel: %s] unable to marshal HCS message because = %s", chain.ChannelID(), err)
				return false
			}
			encrypted, err := chain.encrypt(payload)
			if err != nil {
				logger.Errorf("[channel: %s]cannot enqueue, failed to encrypt message payload: %s", chain.ChannelID(), err)
				return false
			}

			fragments := chain.fragmenter.makeFragments(encrypted, chain.fragmentKey, chain.lastFragmentId)
			chain.lastFragmentId++
			logger.Debugf("[channel: %s] the payload of %d bytes is cut into %d fragments",
				chain.ChannelID(), len(encrypted), len(fragments))
			for _, fragment := range fragments {
				data := protoutil.MarshalOrPanic(fragment)
				if !isResubmission {
					_, err = hedera.NewConsensusMessageSubmitTransaction().
						SetTopicID(chain.topicId).
						SetMessage(data).
						Execute(chain.topicProducer)
					if err != nil {
						logger.Errorf("[channel: %s] cannot enqueue envelope because = %s", chain.ChannelID(), err)
						return false
					}
				} else {
					_, err = hedera.NewConsensusMessageSubmitTransaction().
						SetTopicID(chain.topicId).
						SetMessage(data).
						Execute(chain.singleNodeTopicProducer)
					if err != nil {
						logger.Errorf("[channel: %s] cannot enqueue envelope because = %s", chain.ChannelID(), err)
						return false
					}
				}
				logger.Debugf("[channel: %s] %d fragment of id %d sent successfully", chain.ChannelID(), fragment.Sequence, fragment.FragmentId)
			}
			logger.Debugf("[channel: %s] Envelope enqueued successfully", chain.ChannelID())
			return true
		}
	default: // Not ready yet
		logger.Warningf("[channel: %s] Will not enqueue, consenter for this channel hasn't started yet", chain.ChannelID())
		return false
	}
}

func newConfigMessage(config []byte, configSeq uint64, originalSeq uint64) *ab.HcsMessage {
	return &ab.HcsMessage{
		Type: &ab.HcsMessage_Regular{
			Regular: &ab.HcsMessageRegular{
				Payload:     config,
				ConfigSeq:   configSeq,
				Class:       ab.HcsMessageRegular_CONFIG,
				OriginalSeq: originalSeq,
			},
		},
	}
}

func newNormalMessage(config []byte, configSeq uint64, originalSeq uint64) *ab.HcsMessage {
	return &ab.HcsMessage{
		Type: &ab.HcsMessage_Regular{
			Regular: &ab.HcsMessageRegular{
				Payload:     config,
				ConfigSeq:   configSeq,
				Class:       ab.HcsMessageRegular_NORMAL,
				OriginalSeq: originalSeq,
			},
		},
	}
}

func newHcsMetadata(
	lastConsensusTimestampPersisted *timestamp.Timestamp,
	lastOriginalSequenceProcessed uint64,
	lastResubmittedConfigSequence uint64,
	lastFragmentId uint64) *ab.HcsMetadata {
	return &ab.HcsMetadata{
		LastConsensusTimestampPersisted: lastConsensusTimestampPersisted,
		LastOriginalSequenceProcessed:   lastOriginalSequenceProcessed,
		LastResubmittedConfigSequence:   lastResubmittedConfigSequence,
		LastFragmentId:                  lastFragmentId,
	}
}

func timestampProtoOrPanic(t *time.Time) *timestamp.Timestamp {
	if ts, err := ptypes.TimestampProto(*t); err != nil {
		logger.Panicf("failed to convert time.Time %v to google.protobuf.Timestamp = %v", err)
		return nil
	} else {
		return ts
	}
}

func (chain *chainImpl) doneReprocessing() <-chan struct{} {
	chain.doneReprocessingMutex.Lock()
	defer chain.doneReprocessingMutex.Unlock()
	return chain.doneReprocessingMsgInFlight
}

func (chain *chainImpl) reprocessConfigComplete() {
	chain.doneReprocessingMutex.Lock()
	defer chain.doneReprocessingMutex.Unlock()
	close(chain.doneReprocessingMsgInFlight)
}

func (chain *chainImpl) reprocessConfigPending() {
	chain.doneReprocessingMutex.Lock()
	defer chain.doneReprocessingMutex.Unlock()
	chain.doneReprocessingMsgInFlight = make(chan struct{})
}