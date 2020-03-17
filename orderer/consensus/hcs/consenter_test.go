/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hyperledger/fabric/msp"

	"github.com/golang/protobuf/ptypes/timestamp"
	cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/orderer"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/common/channelconfig"
	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.com/hyperledger/fabric/orderer/consensus"
	"github.com/hyperledger/fabric/orderer/consensus/hcs/mock"
	mockmultichannel "github.com/hyperledger/fabric/orderer/mocks/common/multichannel"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/assert"
)

//go:generate counterfeiter -o mock/orderer_config.go --fake-name OrdererConfig . ordererConfig

type ordererConfig interface {
	channelconfig.Orderer
}

//go:generate counterfeiter -o mock/identity.go --fake-name Identity . identity

type identity interface {
	msp.Identity
}

func init() {
	mockLocalConfig = newMockLocalConfig(false)
	setupTestLogging("DEBUG")
}

func TestNew(t *testing.T) {
	publicIdentity := &mock.Identity{}

	t.Run("Proper", func(t *testing.T) {
		publicIdentity.SerializeReturns(make([]byte, 16), nil)
		c := New(mockLocalConfig.Hcs, publicIdentity)
		_ = consensus.Consenter(c)
	})

	t.Run("IdentityError", func(t *testing.T) {
		publicIdentity.SerializeReturns(nil, fmt.Errorf("can't serialize identity"))
		assert.Panics(t, func() { New(mockLocalConfig.Hcs, publicIdentity) }, "Expected New panics when identity.Serialize returns errors")
	})
}

func TestHandleChain(t *testing.T) {
	publicIdentity := &mock.Identity{}
	publicIdentity.SerializeReturns(make([]byte, 16), nil)
	consenter := New(mockLocalConfig.Hcs, publicIdentity)

	mockOrderer := &mock.OrdererConfig{}
	mockOrdererHcs := &orderer.Hcs{TopicId: "0.0.19718"}
	mockInvalidOrdererHcs := &orderer.Hcs{TopicId: "invalid hcs topic id"}
	mockSupport := &mockmultichannel.ConsenterSupport{
		SharedConfigVal:  mockOrderer,
		ChannelIDVal:     channelNameForTest(t),
		ChannelConfigVal: newMockChannel(),
	}

	zeroTimestamp := timestamp.Timestamp{Seconds: 0, Nanos: 0}
	mockMetadata := &cb.Metadata{Value: protoutil.MarshalOrPanic(&ab.HcsMetadata{
		LastConsensusTimestampPersisted:             &zeroTimestamp,
		LastOriginalSequenceProcessed:               0,
		LastResubmittedConfigSequence:               0,
		LastFragmentFreeConsensusTimestampPersisted: &zeroTimestamp,
	})}
	mockEmptyMetadata := &cb.Metadata{}
	mockCorruptedMetadata := &cb.Metadata{Value: []byte("corrupted data")}
	mockInvalidTimestampMetadata := &cb.Metadata{Value: protoutil.MarshalOrPanic(&ab.HcsMetadata{
		LastConsensusTimestampPersisted:             nil,
		LastOriginalSequenceProcessed:               0,
		LastResubmittedConfigSequence:               0,
		LastFragmentFreeConsensusTimestampPersisted: nil,
	})}

	t.Run("With Valid Config", func(t *testing.T) {
		mockOrderer.HcsReturns(mockOrdererHcs)
		_, err := consenter.HandleChain(mockSupport, mockMetadata)
		assert.NoError(t, err, "Expected the HandleChain call to return without errors")
	})

	t.Run("With Valid Config and Empty Metadata", func(t *testing.T) {
		mockOrderer.HcsReturns(mockOrdererHcs)
		_, err := consenter.HandleChain(mockSupport, mockEmptyMetadata)
		assert.NoError(t, err, "Expected the HandleChain call to return without errors")
	})

	t.Run("With Invalid HCS Topic ID", func(t *testing.T) {
		mockOrderer.HcsReturns(mockInvalidOrdererHcs)
		ch, err := consenter.HandleChain(mockSupport, mockMetadata)
		assert.Nil(t, ch, "Expected the HandleChain call to return a nil chain")
		assert.Error(t, err, "Expected the HandleChain call to return error")
	})

	t.Run("With Corrupted Metadata", func(t *testing.T) {
		mockOrderer.HcsReturns(mockOrdererHcs)
		assert.Panicsf(t, func() { consenter.HandleChain(mockSupport, mockCorruptedMetadata) }, "Expected panic when HandleChain is called")
	})

	t.Run("With Nil Timestamp in Metadata", func(t *testing.T) {
		mockOrderer.HcsReturns(mockOrdererHcs)
		assert.Panics(t, func() { consenter.HandleChain(mockSupport, mockInvalidTimestampMetadata) }, "Expected panic when HandleChain is called")
	})
}

var mockLocalConfig *localconfig.TopLevel

func newMockLocalConfig(enableTLS bool) *localconfig.TopLevel {

	return &localconfig.TopLevel{
		General: localconfig.General{
			TLS: localconfig.TLS{
				Enabled: enableTLS,
			},
		},
		Hcs: localconfig.Hcs{
			Nodes: map[string]string{
				"35.224.154.10:50211": "0.0.4",
				"34.66.20.182:50211":  "0.0.5",
			},
			MirrorNodeAddress: "35.204.38.119:5600",
			Operator: localconfig.HcsOperator{
				Id: "0.0.19651",
				PrivateKey: localconfig.HcsPrivateKey{
					Enabled: true,
					Type:    "ed25519",
					Key:     "302e020100300506032b65700422042043233ca2cf898027adf303f6439bf4a48a16b5be3f695f98b985c1cba9beba43",
				},
			},
		},
	}
}

func setupTestLogging(logLevel string) {
	// This call allows us to (a) get the logging backend initialization that
	// takes place in the `flogging` package, and (b) adjust the verbosity of
	// the logs when running tests on this package.
	spec := fmt.Sprintf("orderer.consensus.hcs=%s", logLevel)
	flogging.ActivateSpec(spec)
}

func channelNameForTest(t *testing.T) string {
	return fmt.Sprintf("%s.channel", strings.Replace(strings.ToLower(t.Name()), "/", ".", -1))
}