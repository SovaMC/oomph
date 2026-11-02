package player

import (
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// legacyActionFlags maps the actions that clients below 1.16.210 report in PlayerAction to the input flags that newer
// clients set in PlayerAuthInput instead.
var legacyActionFlags = map[int32]int{
	protocol.PlayerActionJump:          packet.InputFlagStartJumping,
	protocol.PlayerActionStartSprint:   packet.InputFlagStartSprinting,
	protocol.PlayerActionStopSprint:    packet.InputFlagStopSprinting,
	protocol.PlayerActionStartSneak:    packet.InputFlagStartSneaking,
	protocol.PlayerActionStopSneak:     packet.InputFlagStopSneaking,
	protocol.PlayerActionStartGlide:    packet.InputFlagStartGliding,
	protocol.PlayerActionStopGlide:     packet.InputFlagStopGliding,
	protocol.PlayerActionStartSwimming: packet.InputFlagStartSwimming,
	protocol.PlayerActionStopSwimming:  packet.InputFlagStopSwimming,
}

// legacyInput holds what a client below 1.16.210 sent separately since its last PlayerAuthInput.
type legacyInput struct {
	flags   []int
	flight  []int
	actions []protocol.PlayerBlockAction
}

// UsesLegacyInput returns true if the client reports its actions and block breaking outside of PlayerAuthInput.
func (p *Player) UsesLegacyInput() bool {
	return p.Version > 0 && p.Version < GameVersion1_16_210
}

func (p *Player) usesLegacyItemInteraction() bool {
	return p.Version >= GameVersion1_16_210 && p.Version < GameVersion1_20_0
}

func (p *Player) queueLegacyAction(pk *packet.PlayerAction) {
	if !p.UsesLegacyInput() {
		return
	}
	if flag, ok := legacyActionFlags[pk.ActionType]; ok {
		p.legacyInput.flags = append(p.legacyInput.flags, flag)
		return
	}
	switch pk.ActionType {
	case protocol.PlayerActionStopBreak:
		// These clients finish a break with a transaction. The server treats this action as a finished break, which
		// would skip the check of the transaction, so it only gets to know that the client stopped breaking.
		pk.ActionType = protocol.PlayerActionAbortBreak
	case protocol.PlayerActionStartBreak, protocol.PlayerActionAbortBreak, protocol.PlayerActionCrackBreak:
		p.legacyInput.actions = append(p.legacyInput.actions, protocol.PlayerBlockAction{Action: pk.ActionType, BlockPos: pk.BlockPosition, Face: pk.BlockFace})
	}
}

// queueLegacyFlight turns the flight state reported by clients below 1.20 into the input flags of newer clients. Unlike
// other input these flags stay in the packet: the server ignores the original packet of the client, and the simulation
// only trusts a flight once the server has responded to it.
func (p *Player) queueLegacyFlight(pk *packet.AdventureSettings) {
	if p.Version <= 0 || p.Version >= GameVersion1_20_0 {
		return
	}
	flying := pk.Flags&packet.AdventureFlagFlying != 0
	switch {
	case flying && !p.movement.Flying():
		p.legacyInput.flight = append(p.legacyInput.flight, packet.InputFlagStartFlying)
	case !flying && p.movement.Flying():
		p.legacyInput.flight = append(p.legacyInput.flight, packet.InputFlagStopFlying)
	}
}

// legacyBreakInput takes the queued block actions and returns them in an input that ends with the block break. The
// break must be judged when the transaction arrives, as the transaction reaches the server before the next input.
func (p *Player) legacyBreakInput(tr *protocol.UseItemTransactionData) *packet.PlayerAuthInput {
	actions := append(p.legacyInput.actions, protocol.PlayerBlockAction{Action: protocol.PlayerActionPredictDestroyBlock, BlockPos: tr.BlockPosition, Face: tr.BlockFace})
	p.legacyInput.actions = nil

	pk := &packet.PlayerAuthInput{InputData: protocol.NewInputFlags(packet.InputFlagCount), BlockActions: protocol.Option(actions)}
	pk.InputData.Set(packet.InputFlagPerformBlockActions)
	return pk
}

func brokeBlock(pk *packet.PlayerAuthInput) bool {
	actions, _ := pk.BlockActions.Value()
	return len(actions) > 0 && actions[len(actions)-1].Action == protocol.PlayerActionPredictDestroyBlock
}

// handleLegacyBreak returns false if the block break of a client below 1.16.210 must not reach the server.
func (p *Player) handleLegacyBreak(tr *protocol.UseItemTransactionData) bool {
	if !p.UsesLegacyInput() || tr.ActionType != protocol.UseItemActionBreakBlock {
		return true
	}
	if p.GameMode != packet.GameTypeSurvival && p.GameMode != packet.GameTypeAdventure {
		return true
	}
	pk := p.legacyBreakInput(tr)
	p.handleBlockActions(pk)
	return brokeBlock(pk)
}

// applyLegacyInput moves the queued input into pk so that it is simulated like the input of a newer client. The
// function returned takes it out again: the server already receives the original packets of the client.
func (p *Player) applyLegacyInput(pk *packet.PlayerAuthInput) func() {
	queued := p.legacyInput
	p.legacyInput = legacyInput{}
	for _, flag := range queued.flags {
		pk.InputData.Set(flag)
	}
	for _, flag := range queued.flight {
		pk.InputData.Set(flag)
	}
	if len(queued.actions) > 0 {
		pk.BlockActions = protocol.Option(queued.actions)
		pk.InputData.Set(packet.InputFlagPerformBlockActions)
	}

	interactionBreak := p.applyLegacyItemInteraction(pk)
	return func() {
		for _, flag := range queued.flags {
			pk.InputData.Unset(flag)
		}
		if len(queued.actions) > 0 {
			pk.BlockActions = protocol.Optional[[]protocol.PlayerBlockAction]{}
			pk.InputData.Unset(packet.InputFlagPerformBlockActions)
		}
		if interactionBreak {
			p.restoreLegacyItemInteraction(pk)
		}
	}
}

// applyLegacyItemInteraction adds a block break to the block actions if the client only reported it as an item
// interaction, which clients below 1.20 may do. It returns true if an action was added.
func (p *Player) applyLegacyItemInteraction(pk *packet.PlayerAuthInput) bool {
	if !p.usesLegacyItemInteraction() || !pk.InputData.Load(packet.InputFlagPerformItemInteraction) {
		return false
	}
	data, ok := pk.ItemInteractionData.Value()
	if !ok || data.ActionType != protocol.UseItemActionBreakBlock || p.GameMode == packet.GameTypeCreative {
		return false
	}
	actions, _ := pk.BlockActions.Value()
	for _, action := range actions {
		if action.Action == protocol.PlayerActionPredictDestroyBlock || action.Action == protocol.PlayerActionStopBreak {
			return false
		}
	}
	pk.BlockActions = protocol.Option(append(actions, protocol.PlayerBlockAction{Action: protocol.PlayerActionPredictDestroyBlock, BlockPos: data.BlockPosition, Face: data.BlockFace}))
	pk.InputData.Set(packet.InputFlagPerformBlockActions)
	return true
}

// restoreLegacyItemInteraction removes the action added by applyLegacyItemInteraction. If the simulation already
// filtered it out, the break was refused and the interaction must not reach the server either.
func (p *Player) restoreLegacyItemInteraction(pk *packet.PlayerAuthInput) {
	if !brokeBlock(pk) {
		pk.InputData.Unset(packet.InputFlagPerformItemInteraction)
		return
	}
	actions, _ := pk.BlockActions.Value()
	actions = actions[:len(actions)-1]
	if len(actions) == 0 {
		pk.BlockActions = protocol.Optional[[]protocol.PlayerBlockAction]{}
		pk.InputData.Unset(packet.InputFlagPerformBlockActions)
		return
	}
	pk.BlockActions = protocol.Option(actions)
}
