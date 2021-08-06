/*
SPDX-License-Identifier: Apache-2.0
*/

package factory

import (
	"time"

	"github.com/hashgraph/hedera-sdk-go/v2"
	"google.golang.org/grpc/status"
)

// HcsClientFactory factory to create HcsClient
type HcsClientFactory interface {
	GetHcsClient(
		network map[string]hedera.AccountID,
		mirrorEndpoint string,
		operator *hedera.AccountID,
		privateKey *hedera.PrivateKey,
	) (HcsClient, error)
}

// HcsClient abstracts the methods for Hedera Consensus Service
type HcsClient interface {
	Close() error
	GetConsensusTopicInfo(topicID *hedera.TopicID) (*hedera.TopicInfo, error)
	GetTransactionReceipt(txID *hedera.TransactionID) (*hedera.TransactionReceipt, error)
	Ping(nodeID *hedera.AccountID) error
	SubmitConsensusMessage(message []byte, topicID *hedera.TopicID, txID *hedera.TransactionID) (*hedera.TransactionID, error)
	SubscribeTopic(topicID *hedera.TopicID, startTime *time.Time, endTime *time.Time) (MirrorSubscriptionHandle, error)
}

// MirrorSubscriptionHandle abstracts the methods for mirror node HCS subscription handle
type MirrorSubscriptionHandle interface {
	Unsubscribe()
	Messages() <-chan *hedera.TopicMessage
	Errors() <-chan status.Status
}
