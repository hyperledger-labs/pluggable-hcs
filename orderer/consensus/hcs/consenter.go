/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"fmt"

	"github.com/hashgraph/hedera-sdk-go"
	cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/msp"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.com/hyperledger/fabric/orderer/consensus"
)

var logger = flogging.MustGetLogger("orderer.consensus.hcs")

// New creates a HCS-based consenter. Called by orderer's main.go.
func New(config localconfig.Hcs, publicIdentity msp.Identity) consensus.Consenter {
	logger.Debug("creating HCS-based consenter...")
	identity, err := publicIdentity.Serialize()
	if err != nil {
		logger.Panicf("Failed serializing public identity = %v", err)
	}
	return &consenterImpl{
		sharedHcsConfigVal: &config,
		identityVal:        identity,
	}
}

// consenterImpl holds the implementation of type that satisfies the
// consensus.Consenter interface --as the HandleChain contract requires-- and
// the commonConsenter one.
type consenterImpl struct {
	sharedHcsConfigVal *localconfig.Hcs
	identityVal        []byte
}

// HandleChain creates/returns a reference to a consensus.Chain object for the
// given set of support resources. Implements the consensus.Consenter
// interface. Called by consensus.newChainSupport(), which is itself called by
// multichannel.NewManagerImpl() when ranging over the ledgerFactory's
// existingChains.
func (consenter *consenterImpl) HandleChain(support consensus.ConsenterSupport, metadata *cb.Metadata) (consensus.Chain, error) {
	if _, err := hedera.TopicIDFromString(support.SharedConfig().Hcs().TopicId); err != nil {
		return nil, fmt.Errorf("invalid hcs topic id %s", support.SharedConfig().Hcs().TopicId)
	}

	lastConsensusTimestampPersisted, lastOriginalSequenceProcessed, lastResubmittedConfigSequence, lastFragmentFreeConsensusTimestamp := getStateFromMetadata(metadata.Value, support.ChannelID())
	ch, err := newChain(
		consenter,
		support,
		defaultHcsClientFactory,
		lastConsensusTimestampPersisted,
		lastOriginalSequenceProcessed,
		lastResubmittedConfigSequence,
		lastFragmentFreeConsensusTimestamp,
	)
	if err != nil {
		return nil, err
	}
	return ch, nil
}

// commonConsenter allows us to retrieve the configuration options set on the
// consenter object. These will be common across all chain objects derived by
// this consenter. They are set using local configuration settings. This
// interface is satisfied by consenterImpl.
type commonConsenter interface {
	sharedHcsConfig() *localconfig.Hcs
	identity() []byte
}

func (consenter *consenterImpl) sharedHcsConfig() *localconfig.Hcs {
	return consenter.sharedHcsConfigVal
}

func (consenter *consenterImpl) identity() []byte {
	return consenter.identityVal
}