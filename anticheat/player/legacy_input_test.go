package player

import (
	"github.com/go-gl/mathgl/mgl32"
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

func TestLegacyJumpWaitsForGround(t *testing.T) {
	p := &Player{Version: 419}
	if p.LegacyJumpReady(false, true) {
		t.Fatal("a held jump key must not start a jump")
	}
	if !p.LegacyJumpReady(true, true) {
		t.Fatal("a reported jump on the ground must start")
	}
	if p.LegacyJumpReady(false, true) {
		t.Fatal("a reported jump must start only once")
	}
	if p.LegacyJumpReady(true, false) {
		t.Fatal("a reported jump must not start in the air")
	}
	if !p.LegacyJumpReady(false, true) {
		t.Fatal("a reported jump must start when the simulation lands right after")
	}
	p.LegacyJumpReady(true, false)
	for i := 1; i < legacyJumpFrames; i++ {
		p.LegacyJumpReady(false, false)
	}
	if p.LegacyJumpReady(false, true) {
		t.Fatal("a reported jump must expire")
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

func TestMovementMatches(t *testing.T) {
	predicted := mgl32.Vec3{0.13, -0.0784, 0.2}
	if !movementMatches(predicted, predicted.Add(mgl32.Vec3{0.03, 0, 0.03}), 0.05) {
		t.Fatal("a client moving almost as predicted must match")
	}
	if movementMatches(predicted, predicted.Add(mgl32.Vec3{0.3, 0, 0}), 0.05) {
		t.Fatal("a client moving faster than predicted must not match")
	}
	if movementMatches(mgl32.Vec3{}, mgl32.Vec3{0, 0, 0.14}, 0.05) {
		t.Fatal("a client moving through a block the prediction collides with must not match")
	}
	if movementMatches(predicted, predicted.Add(mgl32.Vec3{0, 0.42, 0}), 0.05) {
		t.Fatal("a client moving up without a predicted jump must not match")
	}
	if movementMatches(predicted, predicted, 0) {
		t.Fatal("a threshold of zero must disable accepting offsets")
	}
}
