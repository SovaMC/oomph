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
	actions []protocol.PlayerBlockAction
}

func (p *Player) usesLegacyInput() bool {
	return p.Version > 0 && p.Version < GameVersion1_16_210
}

func (p *Player) queueLegacyAction(pk *packet.PlayerAction) {
	if !p.usesLegacyInput() {
		return
	}
	if flag, ok := legacyActionFlags[pk.ActionType]; ok {
		p.legacyInput.flags = append(p.legacyInput.flags, flag)
		return
	}
	switch pk.ActionType {
	case protocol.PlayerActionStartBreak, protocol.PlayerActionAbortBreak, protocol.PlayerActionCrackBreak:
		p.legacyInput.actions = append(p.legacyInput.actions, protocol.PlayerBlockAction{Action: pk.ActionType, BlockPos: pk.BlockPosition, Face: pk.BlockFace})
	}
}

func (p *Player) queueLegacyBreak(tr *protocol.UseItemTransactionData) {
	if !p.usesLegacyInput() || tr.ActionType != protocol.UseItemActionBreakBlock {
		return
	}
	p.legacyInput.actions = append(p.legacyInput.actions, protocol.PlayerBlockAction{Action: protocol.PlayerActionPredictDestroyBlock, BlockPos: tr.BlockPosition, Face: tr.BlockFace})
}

// applyLegacyInput moves the queued input into pk so that it is simulated like the input of a newer client. The
// function returned takes it out again: the server already receives the original packets of the client.
func (p *Player) applyLegacyInput(pk *packet.PlayerAuthInput) func() {
	queued := p.legacyInput
	p.legacyInput = legacyInput{}
	for _, flag := range queued.flags {
		pk.InputData.Set(flag)
	}
	if len(queued.actions) > 0 {
		pk.BlockActions = protocol.Option(queued.actions)
		pk.InputData.Set(packet.InputFlagPerformBlockActions)
	}
	return func() {
		for _, flag := range queued.flags {
			pk.InputData.Unset(flag)
		}
		if len(queued.actions) > 0 {
			pk.BlockActions = protocol.Optional[[]protocol.PlayerBlockAction]{}
			pk.InputData.Unset(packet.InputFlagPerformBlockActions)
		}
	}
}
