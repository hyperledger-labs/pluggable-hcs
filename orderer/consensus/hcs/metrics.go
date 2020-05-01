/*
SPDX-License-Identifier: Apache-2.0
*/

package hcs

import "github.com/hyperledger/fabric/common/metrics"

var (
	numberNodesOpts = metrics.GaugeOpts{
		Namespace:    "consensus",
		Subsystem:    "hcs",
		Name:         "nodes_number",
		Help:         "Number of nodes in this channel",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
	}
	committedBlockNumberOpts = metrics.GaugeOpts{
		Namespace:    "consensus",
		Subsystem:    "hcs",
		Name:         "committed_block_number",
		Help:         "The block number of the latest block committed.",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
	}
	lastConsensusTimestampPersistedOpts = metrics.GaugeOpts{
		Namespace:    "consensus",
		Subsystem:    "hcs",
		Name:         "last_consensus_timestamp_persisted",
		Help:         "The consensus timestamp in the metadata of the last committed block",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
	}
	numberMessagesDroppedOpts = metrics.CounterOpts{
		Namespace:    "consensus",
		Subsystem:    "hcs",
		Name:         "messages_dropped",
		Help:         "The total number of dropped messages",
		LabelNames:   []string{"channel"},
		StatsdFormat: "%{#fqname}.%{channel}",
	}
)

type Metrics struct {
	NumberNodes                     metrics.Gauge
	CommittedBlockNumber            metrics.Gauge
	LastConsensusTimestampPersisted metrics.Gauge
	NumberMessagesDropped           metrics.Counter
}

func NewMetrics(p metrics.Provider) *Metrics {
	return &Metrics{
		NumberNodes:                     p.NewGauge(numberNodesOpts),
		CommittedBlockNumber:            p.NewGauge(committedBlockNumberOpts),
		LastConsensusTimestampPersisted: p.NewGauge(lastConsensusTimestampPersistedOpts),
		NumberMessagesDropped:           p.NewCounter(numberMessagesDroppedOpts),
	}
}
