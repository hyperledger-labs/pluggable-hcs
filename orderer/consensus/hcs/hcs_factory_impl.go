/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"time"

	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/hyperledger/fabric/orderer/consensus/hcs/factory"
	"github.com/pkg/errors"
	"google.golang.org/grpc/status"
)

// implements factory.HcsClientFactory
type hcsClientFactoryImpl struct{}

func (f *hcsClientFactoryImpl) GetHcsClient(
	network map[string]hedera.AccountID,
	mirrorEndpoint string,
	operator *hedera.AccountID,
	privateKey *hedera.PrivateKey,
) (factory.HcsClient, error) {
	if network == nil || len(network) == 0 {
		return nil, errors.Errorf("invalid network")
	}

	if mirrorEndpoint == "" {
		return nil, errors.Errorf("empty mirror endpoint")
	}

	client := hedera.ClientForNetwork(network)
	client.SetMirrorNetwork([]string{mirrorEndpoint})
	client.SetOperator(*operator, *privateKey)
	return &hcsClientImpl{client}, nil
}

// implements factory.HcsClient
type hcsClientImpl struct {
	client *hedera.Client
}

func (c *hcsClientImpl) Close() error {
	return c.client.Close()
}

func (c *hcsClientImpl) SubmitConsensusMessage(
	message []byte,
	topicID *hedera.TopicID,
	txID *hedera.TransactionID,
) (*hedera.TransactionID, error) {
	tx := hedera.NewTopicMessageSubmitTransaction().
		SetTopicID(*topicID).
		SetMessage(message)
	if txID != nil {
		tx.SetTransactionID(*txID)
	}

	resp, err := tx.Execute(c.client)
	if err != nil {
		return nil, err
	}

	return &resp.TransactionID, nil
}

func (c *hcsClientImpl) GetConsensusTopicInfo(topicID *hedera.TopicID) (*hedera.TopicInfo, error) {
	info, err := hedera.NewTopicInfoQuery().
		SetTopicID(*topicID).
		Execute(c.client)
	return &info, err
}

func (c *hcsClientImpl) GetTransactionReceipt(txID *hedera.TransactionID) (*hedera.TransactionReceipt, error) {
	receipt, err := txID.GetReceipt(c.client)
	return &receipt, err
}

func (c *hcsClientImpl) Ping(nodeID *hedera.AccountID) error {
	return c.client.Ping(*nodeID)
}

func (c *hcsClientImpl) SubscribeTopic(
	topicID *hedera.TopicID,
	startTime *time.Time,
	endTime *time.Time,
) (factory.MirrorSubscriptionHandle, error) {
	handle := newMirrorSubscriptionHandle()

	// disable SDK retry
	retryHandler := func(err error) bool {
		return false
	}
	query := hedera.NewTopicMessageQuery().
		SetErrorHandler(handle.onError).
		SetTopicID(*topicID).
		SetRetryHandler(retryHandler)
	if startTime != nil {
		query.SetStartTime(*startTime)
	}
	if endTime != nil {
		query.SetEndTime(*endTime)
	}

	var err error
	if handle.SubscriptionHandle, err = query.Subscribe(c.client, handle.onNext); err != nil {
		return nil, err
	}

	return handle, nil
}

// implements factory.MirrorSubscriptionHandle
type mirrorSubscriptionHandleImpl struct {
	hedera.SubscriptionHandle
	errChan chan status.Status
	msgChan chan *hedera.TopicMessage
	done    chan struct{}
}

func newMirrorSubscriptionHandle() *mirrorSubscriptionHandleImpl {
	return &mirrorSubscriptionHandleImpl{
		errChan: make(chan status.Status),
		msgChan: make(chan *hedera.TopicMessage),
		done:    make(chan struct{}),
	}
}

func (h *mirrorSubscriptionHandleImpl) Unsubscribe() {
	select {
	case <-h.done:
	default:
		h.SubscriptionHandle.Unsubscribe()
		close(h.msgChan)
		close(h.errChan)
		close(h.done)
	}
}

func (h *mirrorSubscriptionHandleImpl) Messages() <-chan *hedera.TopicMessage {
	return h.msgChan
}

func (h *mirrorSubscriptionHandleImpl) Errors() <-chan status.Status {
	return h.errChan
}

func (h *mirrorSubscriptionHandleImpl) onNext(message hedera.TopicMessage) {
	h.msgChan <- &message
}

func (h *mirrorSubscriptionHandleImpl) onError(status status.Status) {
	h.errChan <- status
}
