package factory

import (
	"github.com/hashgraph/hedera-sdk-go"
	"time"
)

// hcs abstraction layer
type HcsClientFactory interface {
	GetConsensusClient(network map[string]hedera.AccountID, operator *hedera.AccountID, privateKey *hedera.Ed25519PrivateKey) (ConsensusClient, error)
	GetMirrorClient(address string) (MirrorClient, error)
}

type ConsensusClient interface {
	Close() error
	SubmitConsensusMessage(message []byte, topicID *hedera.ConsensusTopicID) error
}

type MirrorClient interface {
	Close() error
	SubscribeTopic(
		topicID *hedera.ConsensusTopicID,
		startTime *time.Time,
		endTime *time.Time,
	) (MirrorSubscriptionHandle, error)
}

type MirrorSubscriptionHandle interface {
	Unsubscribe()
	Responses() <-chan *hedera.MirrorConsensusTopicResponse
	Errors() <-chan error
}
