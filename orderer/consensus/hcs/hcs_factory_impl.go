package hcs

import (
	"github.com/hashgraph/hedera-sdk-go"
	"github.com/hyperledger/fabric/orderer/consensus/hcs/factory"
	"github.com/pkg/errors"
	"time"
)

// implements factory.HcsClientFactory
type hcsClientFactoryImpl struct{}

func (f *hcsClientFactoryImpl) GetConsensusClient(
	network map[string]hedera.AccountID,
	operator hedera.AccountID,
	privateKey hedera.Ed25519PrivateKey,
) (factory.ConsensusClient, error) {
	if network == nil || len(network) == 0 {
		return nil, errors.Errorf("invalid network")
	}
	client := hedera.NewClient(network)
	client.SetOperator(operator, privateKey)
	return &consensusClientImpl{client}, nil
}

func (f *hcsClientFactoryImpl) GetMirrorClient(endpoint string) (factory.MirrorClient, error) {
	if client, err := hedera.NewMirrorClient(endpoint); err != nil {
		return nil, err
	} else {
		return &mirrorClientImpl{&client}, nil
	}
}

// implements factory.ConsensusClient
type consensusClientImpl struct {
	client *hedera.Client
}

func (c *consensusClientImpl) Close() error {
	return c.client.Close()
}

func (c *consensusClientImpl) SubmitConsensusMessage(message []byte, topicId hedera.ConsensusTopicID) error {
	_, err := hedera.NewConsensusMessageSubmitTransaction().
		SetTopicID(topicId).
		SetMessage(message).
		Execute(c.client)
	return err
}

// implements factory.MirrorClient
type mirrorClientImpl struct {
	mc *hedera.MirrorClient
}

func (c *mirrorClientImpl) Close() error {
	return c.mc.Close()
}

func (c *mirrorClientImpl) SubscribeTopic(
	topicId hedera.ConsensusTopicID,
	startTime *time.Time,
	endTime *time.Time,
) (factory.MirrorSubscriptionHandle, error) {
	handle := newMirrorSubscriptionHandle()
	onNext := func(resp hedera.MirrorConsensusTopicResponse) {
		handle.onNext(&resp)
	}
	onError := func(err error) {
		handle.onError(err)
	}

	query := hedera.NewMirrorConsensusTopicQuery().SetTopicID(topicId)
	if startTime != nil {
		query.SetStartTime(*startTime)
	}
	if endTime != nil {
		query.SetEndTime(*endTime)
	}
	var err error
	if handle.MirrorSubscriptionHandle, err = query.Subscribe(*c.mc, onNext, onError); err != nil {
		return nil, err
	}
	return handle, nil
}

// implements factory.MirrorSubscriptionHandle
type mirrorSubscriptionHandleImpl struct {
	hedera.MirrorSubscriptionHandle
	errChan  chan error
	respChan chan *hedera.MirrorConsensusTopicResponse
}

func newMirrorSubscriptionHandle() *mirrorSubscriptionHandleImpl {
	return &mirrorSubscriptionHandleImpl{
		errChan:  make(chan error),
		respChan: make(chan *hedera.MirrorConsensusTopicResponse),
	}
}

func (h *mirrorSubscriptionHandleImpl) Unsubscribe() {
	h.MirrorSubscriptionHandle.Unsubscribe()
	close(h.errChan)
	close(h.respChan)
}

func (h *mirrorSubscriptionHandleImpl) Responses() <-chan *hedera.MirrorConsensusTopicResponse {
	return h.respChan
}

func (h *mirrorSubscriptionHandleImpl) Errors() <-chan error {
	return h.errChan
}

func (h *mirrorSubscriptionHandleImpl) onNext(resp *hedera.MirrorConsensusTopicResponse) {
	h.respChan <- resp
}

func (h *mirrorSubscriptionHandleImpl) onError(err error) {
	h.errChan <- err
}
