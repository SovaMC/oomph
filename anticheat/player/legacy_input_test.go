package player

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestLegacyInputIsAppliedForOneInput(t *testing.T) {
	p := &Player{Version: 419}
	pos := protocol.BlockPos{1, 2, 3}
	p.queueLegacyAction(&packet.PlayerAction{ActionType: protocol.PlayerActionStartSprint})
	p.queueLegacyAction(&packet.PlayerAction{ActionType: protocol.PlayerActionJump})
	p.queueLegacyAction(&packet.PlayerAction{ActionType: protocol.PlayerActionStartBreak, BlockPosition: pos, BlockFace: 1})
	p.queueLegacyBreak(&protocol.UseItemTransactionData{ActionType: protocol.UseItemActionBreakBlock, BlockPosition: pos, BlockFace: 1})
	p.queueLegacyBreak(&protocol.UseItemTransactionData{ActionType: protocol.UseItemActionClickBlock, BlockPosition: pos, BlockFace: 1})

	pk := &packet.PlayerAuthInput{InputData: protocol.NewInputFlags(packet.InputFlagCount)}
	restore := p.applyLegacyInput(pk)
	for _, flag := range []int{packet.InputFlagStartSprinting, packet.InputFlagStartJumping, packet.InputFlagPerformBlockActions} {
		if !pk.InputData.Load(flag) {
			t.Fatalf("flag %d was not applied", flag)
		}
	}
	actions, _ := pk.BlockActions.Value()
	if len(actions) != 2 || actions[0].Action != protocol.PlayerActionStartBreak || actions[1].Action != protocol.PlayerActionPredictDestroyBlock || actions[1].BlockPos != pos {
		t.Fatalf("unexpected block actions: %+v", actions)
	}

	restore()
	for _, flag := range []int{packet.InputFlagStartSprinting, packet.InputFlagStartJumping, packet.InputFlagPerformBlockActions} {
		if pk.InputData.Load(flag) {
			t.Fatalf("flag %d leaked to the server", flag)
		}
	}
	if _, ok := pk.BlockActions.Value(); ok {
		t.Fatal("block actions leaked to the server")
	}

	next := &packet.PlayerAuthInput{InputData: protocol.NewInputFlags(packet.InputFlagCount)}
	p.applyLegacyInput(next)
	if next.InputData.Load(packet.InputFlagStartSprinting) {
		t.Fatal("queued input was applied twice")
	}
}

func TestLegacyInputIgnoredOnNewerClients(t *testing.T) {
	for _, version := range []int32{0, 475, 486, GameVersion1_21_40} {
		p := &Player{Version: version}
		p.queueLegacyAction(&packet.PlayerAction{ActionType: protocol.PlayerActionStartSprint})
		p.queueLegacyBreak(&protocol.UseItemTransactionData{ActionType: protocol.UseItemActionBreakBlock})
		if len(p.legacyInput.flags) != 0 || len(p.legacyInput.actions) != 0 {
			t.Fatalf("version %d queued legacy input", version)
		}
	}
}
