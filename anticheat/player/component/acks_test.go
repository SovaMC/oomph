package component

import (
	"testing"

	"github.com/oomph-ac/oomph/anticheat/player"
)

type countingACK struct{ runs *int }

func (a countingACK) Run() { *a.runs++ }

func TestLegacyACKResponses(t *testing.T) {
	for name, response := range map[string]func(sent int64) int64{
		"unchanged":    func(sent int64) int64 { return sent },
		"times 1000":   func(sent int64) int64 { return sent * AckDivider },
		"times 1000^2": func(sent int64) int64 { return sent * AckDivider * AckDivider },
	} {
		t.Run(name, func(t *testing.T) {
			var runs int
			ack := &ACKComponent{legacyMode: true, mPlayer: player.New(nil, player.MonitoringState{}, nil)}
			ack.Refresh()
			ack.Add(countingACK{runs: &runs})
			sent := ack.currentBatch.timestamp
			if sent%AckDivider != 0 {
				t.Fatalf("legacy timestamp %d is not a multiple of %d", sent, AckDivider)
			}
			ack.pending = append(ack.pending, ack.currentBatch)
			if !ack.Execute(response(sent)) || runs != 1 {
				t.Fatalf("response was not matched: runs=%d", runs)
			}
		})
	}
}

func TestSetLegacyKeepsQueuedACKs(t *testing.T) {
	var runs int
	ack := &ACKComponent{mPlayer: player.New(nil, player.MonitoringState{}, nil)}
	ack.Refresh()
	ack.Add(countingACK{runs: &runs})
	ack.SetLegacy(true)
	if len(ack.currentBatch.acks) != 1 || ack.currentBatch.timestamp%AckDivider != 0 {
		t.Fatalf("unexpected batch after SetLegacy: %+v", ack.currentBatch)
	}
}
