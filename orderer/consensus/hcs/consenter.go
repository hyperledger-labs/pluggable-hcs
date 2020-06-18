/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"fmt"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hashgraph/hedera-sdk-go"
	"github.com/hyperledger/fabric-lib-go/healthz"
	cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/common/metrics"
	"github.com/hyperledger/fabric/msp"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.com/hyperledger/fabric/orderer/consensus"
	hb "github.com/hyperledger/fabric/orderer/consensus/hcs/protodef"
)

var logger = flogging.MustGetLogger("orderer.consensus.hcs")

//go:generate counterfeiter -o mock/health_checker.go -fake-name HealthChecker . healthChecker

// healthChecker defines the contract for health checker
type healthChecker interface {
	RegisterChecker(component string, checker healthz.HealthChecker) error
}

// New creates a HCS-based consenter. Called by orderer's main.go.
func New(config localconfig.Hcs, publicIdentity msp.Identity, metricsProvider metrics.Provider, healthChecker healthChecker) consensus.Consenter {
	logger.Debug("creating HCS-based consenter...")
	identity, err := publicIdentity.Serialize()
	if err != nil {
		logger.Panicf("Failed serializing public identity = %v", err)
	}
	return &consenterImpl{
		sharedHcsConfigVal: &config,
		identityVal:        identity,
		metrics:            NewMetrics(metricsProvider),
		healthChecker:      healthChecker,
		topicChannelMap:    make(map[string]string),
	}
}

// consenterImpl holds the implementation of type that satisfies the
// consensus.Consenter interface --as the HandleChain contract requires-- and
// the commonConsenter one.
type consenterImpl struct {
	validStartMutex    sync.Mutex
	nextValidStart     time.Time
	sharedHcsConfigVal *localconfig.Hcs
	identityVal        []byte
	metrics            *Metrics
	healthChecker      healthChecker
	topicChannelMap    map[string]string
}

// HandleChain creates/returns a reference to a consensus.Chain object for the
// given set of support resources. Implements the consensus.Consenter
// interface. Called by consensus.newChainSupport(), which is itself called by
// multichannel.NewManagerImpl() when ranging over the ledgerFactory's
// existingChains.
func (consenter *consenterImpl) HandleChain(support consensus.ConsenterSupport, metadata *cb.Metadata) (ch consensus.Chain, err error) {
	var topicID hedera.ConsensusTopicID
	defer func() {
		if err == nil {
			consenter.topicChannelMap[topicID.String()] = support.ChannelID()
		}
	}()

	configMetadata := &hb.HcsConfigMetadata{}
	if proto.Unmarshal(support.SharedConfig().ConsensusMetadata(), configMetadata) != nil {
		return nil, fmt.Errorf("cannot unmarshal config metadata = %v", err)
	}
	if topicID, err = hedera.TopicIDFromString(configMetadata.TopicId); err != nil {
		return nil, fmt.Errorf("invalid HCS Topic ID = %v", err)
	}
	if channelID, ok := consenter.topicChannelMap[configMetadata.TopicId]; ok {
		return nil, fmt.Errorf("HCS Topic ID %s is already used for channel %s", configMetadata.TopicId, channelID)
	}

	lastConsensusTimestampPersisted, lastOriginalSequenceProcessed, lastResubmittedConfigSequence, lastChunkFreeConsensusTimestamp, lastChunkFreeSequenceProcessed := getStateFromMetadata(metadata.Value, support.ChannelID())
	return newChain(
		consenter,
		support,
		consenter.healthChecker,
		defaultHcsClientFactory,
		lastConsensusTimestampPersisted,
		lastOriginalSequenceProcessed,
		lastResubmittedConfigSequence,
		lastChunkFreeConsensusTimestamp,
		lastChunkFreeSequenceProcessed,
	)
}

// commonConsenter allows us to retrieve the configuration options set on the
// consenter object. These will be common across all chain objects derived by
// this consenter. They are set using local configuration settings. This
// interface is satisfied by consenterImpl.
type commonConsenter interface {
	getUniqueValidStart(time.Time) time.Time
	sharedHcsConfig() *localconfig.Hcs
	identity() []byte
	Metrics() *Metrics
}

func (consenter *consenterImpl) getUniqueValidStart(wanted time.Time) time.Time {
	consenter.validStartMutex.Lock()
	defer consenter.validStartMutex.Unlock()
	if wanted.After(consenter.nextValidStart) {
		consenter.nextValidStart = wanted
	}
	validStart := consenter.nextValidStart
	consenter.nextValidStart = consenter.nextValidStart.Add(time.Nanosecond)
	return validStart
}

func (consenter *consenterImpl) sharedHcsConfig() *localconfig.Hcs {
	return consenter.sharedHcsConfigVal
}

func (consenter *consenterImpl) identity() []byte {
	return consenter.identityVal
}

func (consenter *consenterImpl) Metrics() *Metrics {
	return consenter.metrics
}
