/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import (
	"github.com/hyperledger/fabric/common/metrics/metricsfakes"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNewMetrics(t *testing.T) {
	fakeProvider := &metricsfakes.Provider{}
	fakeGauge := &metricsfakes.Gauge{}
	fakeCounter := &metricsfakes.Counter{}

	fakeProvider.NewGaugeReturns(fakeGauge)
	fakeProvider.NewCounterReturns(fakeCounter)

	metrics := NewMetrics(fakeProvider)
	assert.Equal(t, fakeGauge, metrics.NumberNodes)
	assert.Equal(t, fakeGauge, metrics.CommittedBlockNumber)
	assert.Equal(t, fakeGauge, metrics.LastConsensusTimestampPersisted)
	assert.Equal(t, fakeCounter, metrics.NumberMessagesDropped)
}

// helper functions for testing purpose

func newFakeMetrics(fakeFields *fakeMetricsFields) *Metrics {
	return &Metrics{
		NumberNodes:                     fakeFields.fakeNumberNodes,
		CommittedBlockNumber:            fakeFields.fakeCommittedBlockNumber,
		LastConsensusTimestampPersisted: fakeFields.fakeLastConsensusTimestampPersisted,
		NumberMessagesDropped:           fakeFields.fakeMessagesDropped,
	}
}

type fakeMetricsFields struct {
	fakeNumberNodes                     *metricsfakes.Gauge
	fakeCommittedBlockNumber            *metricsfakes.Gauge
	fakeLastConsensusTimestampPersisted *metricsfakes.Gauge
	fakeMessagesDropped                 *metricsfakes.Counter
}

func newFakeMetricsFields() *fakeMetricsFields {
	return &fakeMetricsFields{
		fakeNumberNodes:                     newFakeGauge(),
		fakeCommittedBlockNumber:            newFakeGauge(),
		fakeLastConsensusTimestampPersisted: newFakeGauge(),
		fakeMessagesDropped:                 newFakeCounter(),
	}
}

func newFakeGauge() *metricsfakes.Gauge {
	fakeGauge := &metricsfakes.Gauge{}
	fakeGauge.WithReturns(fakeGauge)
	return fakeGauge
}

func newFakeCounter() *metricsfakes.Counter {
	fakeCounter := &metricsfakes.Counter{}
	fakeCounter.WithReturns(fakeCounter)
	return fakeCounter
}
