/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"fmt"
	"github.com/hyperledger/fabric/common/metrics/disabled"
	"strings"
	"testing"

	"github.com/hyperledger/fabric/msp"

	"github.com/golang/protobuf/ptypes/timestamp"
	cb "github.com/hyperledger/fabric-protos-go/common"
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

const testOperatorPrivateKey = "302e020100300506032b657004220420e373811ccb438637a4358db3cbb72dd899eeda6b764c0b8128c61063752b4fe4"

func init() {
	mockLocalConfig = newMockLocalConfig(false)
	setupTestLogging("ERROR")
}

func TestNew(t *testing.T) {
	publicIdentity := &mock.Identity{}
	healthChecker := &mock.HealthChecker{}

	t.Run("Proper", func(t *testing.T) {
		publicIdentity.SerializeReturns(make([]byte, 16), nil)
		c := New(mockLocalConfig.Hcs, publicIdentity, &disabled.Provider{}, healthChecker)
		_ = consensus.Consenter(c)
	})

	t.Run("IdentityError", func(t *testing.T) {
		publicIdentity.SerializeReturns(nil, fmt.Errorf("can't serialize identity"))
		assert.Panics(t, func() { New(mockLocalConfig.Hcs, publicIdentity, &disabled.Provider{}, healthChecker) }, "Expected New panics when identity.Serialize returns errors")
	})
}

func TestHandleChain(t *testing.T) {
	publicIdentity := &mock.Identity{}
	publicIdentity.SerializeReturns(make([]byte, 16), nil)
	healthChecker := &mock.HealthChecker{}
	consenter := New(mockLocalConfig.Hcs, publicIdentity, &disabled.Provider{}, healthChecker)

	mockOrderer := &mock.OrdererConfig{}
	mockConfigMetadata := protoutil.MarshalOrPanic(&ab.HcsConfigMetadata{TopicID: "0.0.19718"})
	mockInvalidConfigMetadata := protoutil.MarshalOrPanic(&ab.HcsConfigMetadata{TopicID: "invalid hcs topic id"})
	mockSupport := &mockmultichannel.ConsenterSupport{
		SharedConfigVal:  mockOrderer,
		ChannelIDVal:     channelNameForTest(t),
		ChannelConfigVal: newMockChannel(),
	}

	zeroTimestamp := timestamp.Timestamp{Seconds: 0, Nanos: 0}
	mockBlockMetadata := &cb.Metadata{Value: protoutil.MarshalOrPanic(&ab.HcsMetadata{
		LastConsensusTimestampPersisted:             &zeroTimestamp,
		LastOriginalSequenceProcessed:               0,
		LastResubmittedConfigSequence:               0,
		LastFragmentFreeConsensusTimestampPersisted: &zeroTimestamp,
	})}
	mockInvalidTimestampBlockMetadata := &cb.Metadata{Value: protoutil.MarshalOrPanic(&ab.HcsMetadata{
		LastConsensusTimestampPersisted:             nil,
		LastOriginalSequenceProcessed:               0,
		LastResubmittedConfigSequence:               0,
		LastFragmentFreeConsensusTimestampPersisted: nil,
	})}

	var tests = []struct {
		name           string
		configMetadata []byte
		blockMetadata  *cb.Metadata
		expectError    bool
		expectPanic    bool
	}{
		{
			name:           "WithValidConfig",
			configMetadata: mockConfigMetadata,
			blockMetadata:  mockBlockMetadata,
			expectError:    false,
			expectPanic:    false,
		},
		{
			name:           "WithValidConfigAndEmptyMetadata",
			configMetadata: mockConfigMetadata,
			blockMetadata:  &cb.Metadata{},
			expectError:    false,
			expectPanic:    false,
		},
		{
			name:           "WithInvalidHCSTopicID",
			configMetadata: mockInvalidConfigMetadata,
			blockMetadata:  mockBlockMetadata,
			expectError:    true,
			expectPanic:    false,
		},
		{
			name:           "WithCorruptedConfigMetadata",
			configMetadata: []byte("corrupted config metadata"),
			blockMetadata:  mockBlockMetadata,
			expectError:    true,
			expectPanic:    false,
		},
		{
			name:           "WithCorruptedBlockMetadata",
			configMetadata: mockConfigMetadata,
			blockMetadata:  &cb.Metadata{Value: []byte("corrupted data")},
			expectError:    false,
			expectPanic:    true,
		},
		{
			name:           "WithNilTimestampInMetadata",
			configMetadata: mockConfigMetadata,
			blockMetadata:  mockInvalidTimestampBlockMetadata,
			expectError:    false,
			expectPanic:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mockOrderer.ConsensusMetadataReturns(test.configMetadata)
			if test.expectPanic {
				assert.Panics(t, func() { consenter.HandleChain(mockSupport, test.blockMetadata) })
			} else {
				ch, err := consenter.HandleChain(mockSupport, test.blockMetadata)
				if test.expectError {
					assert.Nil(t, ch)
					assert.Error(t, err)
				} else {
					assert.NotNil(t, ch)
					assert.NoError(t, err)
				}
			}
		})
	}
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
					Type: "ed25519",
					Key:  testOperatorPrivateKey,
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
