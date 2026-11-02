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

	pk := &packet.PlayerAuthInput{InputData: protocol.NewInputFlags(packet.InputFlagCount)}
	restore := p.applyLegacyInput(pk)
	for _, flag := range []int{packet.InputFlagStartSprinting, packet.InputFlagStartJumping, packet.InputFlagPerformBlockActions} {
		if !pk.InputData.Load(flag) {
			t.Fatalf("flag %d was not applied", flag)
		}
	}
	actions, _ := pk.BlockActions.Value()
	if len(actions) != 1 || actions[0].Action != protocol.PlayerActionStartBreak || actions[0].BlockPos != pos {
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
		if len(p.legacyInput.flags) != 0 || len(p.legacyInput.actions) != 0 {
			t.Fatalf("version %d queued legacy input", version)
		}
	}
}

func TestLegacyBreakUsesQueuedActions(t *testing.T) {
	p := &Player{Version: 419}
	pos := protocol.BlockPos{1, 2, 3}
	p.queueLegacyAction(&packet.PlayerAction{ActionType: protocol.PlayerActionStartBreak, BlockPosition: pos, BlockFace: 1})
	stop := &packet.PlayerAction{ActionType: protocol.PlayerActionStopBreak, BlockPosition: pos, BlockFace: 1}
	p.queueLegacyAction(stop)
	if stop.ActionType != protocol.PlayerActionAbortBreak {
		t.Fatal("stop action would finish the break on the server")
	}

	pk := p.legacyBreakInput(&protocol.UseItemTransactionData{ActionType: protocol.UseItemActionBreakBlock, BlockPosition: pos, BlockFace: 1})
	actions, _ := pk.BlockActions.Value()
	if len(actions) != 2 || actions[0].Action != protocol.PlayerActionStartBreak || actions[1].Action != protocol.PlayerActionPredictDestroyBlock || actions[1].BlockPos != pos {
		t.Fatalf("unexpected block actions: %+v", actions)
	}
	if !brokeBlock(pk) {
		t.Fatal("break was not recognised")
	}
	if len(p.legacyInput.actions) != 0 {
		t.Fatal("queued actions were not consumed")
	}

	pk.BlockActions = protocol.Option(actions[:1])
	if brokeBlock(pk) {
		t.Fatal("refused break was recognised")
	}
}

func TestLegacyItemInteractionBreak(t *testing.T) {
	pos := protocol.BlockPos{1, 2, 3}
	input := func() *packet.PlayerAuthInput {
		pk := &packet.PlayerAuthInput{InputData: protocol.NewInputFlags(packet.InputFlagCount)}
		pk.InputData.Set(packet.InputFlagPerformItemInteraction)
		pk.ItemInteractionData = protocol.Option(protocol.UseItemTransactionData{ActionType: protocol.UseItemActionBreakBlock, BlockPosition: pos, BlockFace: 1})
		return pk
	}

	p := &Player{Version: 486, GameMode: packet.GameTypeSurvival}
	pk := input()
	restore := p.applyLegacyInput(pk)
	actions, _ := pk.BlockActions.Value()
	if len(actions) != 1 || actions[0].Action != protocol.PlayerActionPredictDestroyBlock || !pk.InputData.Load(packet.InputFlagPerformBlockActions) {
		t.Fatalf("break was not added: %+v", actions)
	}
	restore()
	if _, ok := pk.BlockActions.Value(); ok || pk.InputData.Load(packet.InputFlagPerformBlockActions) {
		t.Fatal("added break leaked to the server")
	}
	if !pk.InputData.Load(packet.InputFlagPerformItemInteraction) {
		t.Fatal("accepted interaction was removed")
	}

	pk = input()
	restore = p.applyLegacyInput(pk)
	pk.BlockActions = protocol.Optional[[]protocol.PlayerBlockAction]{}
	restore()
	if pk.InputData.Load(packet.InputFlagPerformItemInteraction) {
		t.Fatal("refused interaction reached the server")
	}

	for _, version := range []int32{419, GameVersion1_21_40} {
		pk = input()
		(&Player{Version: version, GameMode: packet.GameTypeSurvival}).applyLegacyInput(pk)
		if _, ok := pk.BlockActions.Value(); ok {
			t.Fatalf("version %d converted an item interaction", version)
		}
	}
}

func TestLegacyFlightFlagsReachTheServer(t *testing.T) {
	p := &Player{Version: 419, legacyInput: legacyInput{flags: []int{packet.InputFlagStartSprinting}, flight: []int{packet.InputFlagStartFlying}}}
	pk := &packet.PlayerAuthInput{InputData: protocol.NewInputFlags(packet.InputFlagCount)}
	p.applyLegacyInput(pk)()
	if !pk.InputData.Load(packet.InputFlagStartFlying) {
		t.Fatal("flight flag did not reach the server")
	}
	if pk.InputData.Load(packet.InputFlagStartSprinting) {
		t.Fatal("sprint flag leaked to the server")
	}
}
