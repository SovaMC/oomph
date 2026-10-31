package blocknetwork

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/sandertv/gophertunnel/minecraft"
)

// chunkFormat describes chunk payloads that the server wrote for a specific client protocol. Such payloads may carry
// the protocol's own block runtime IDs and vertical range, while single block packets keep canonical runtime IDs.
type chunkFormat struct {
	key    uint64
	unmap  func(uint32) (uint32, bool)
	air    uint32
	r      cube.Range
	ranged bool
}

// WithChunkProtocol returns a copy of the Codec that reads chunk payloads written for proto. Protocols that pre-map
// chunk palettes expose UnmapBlockRuntimeID, and historical layouts expose NetworkChunkRange.
func (c Codec) WithChunkProtocol(proto minecraft.Protocol) Codec {
	if proto == nil {
		return c
	}
	f := chunkFormat{key: uint64(uint32(proto.ID()))}
	if m, ok := proto.(interface{ UnmapBlockRuntimeID(uint32) (uint32, bool) }); ok {
		f.unmap = m.UnmapBlockRuntimeID
		f.air, _ = c.registry.StateToRuntimeID("minecraft:air", nil)
	}
	if r, ok := proto.(interface{ NetworkChunkRange() (int16, int16) }); ok {
		minY, maxY := r.NetworkChunkRange()
		f.r, f.ranged = cube.Range{int(minY), int(maxY)}, true
	}
	c.chunks = f
	return c
}

// ChunkKey returns a value that distinguishes chunk payloads that decode differently for different codecs.
func (c Codec) ChunkKey() uint64 {
	return uint64(c.mode) | c.chunks.key<<8
}

// MapsChunks reports whether chunk payloads carry protocol-specific block runtime IDs.
func (c Codec) MapsChunks() bool {
	return c.chunks.unmap != nil
}

// ChunkRange returns the vertical range chunk payloads are laid out for, if it differs from the dimension's range.
func (c Codec) ChunkRange() (cube.Range, bool) {
	return c.chunks.r, c.chunks.ranged
}

// UnmapChunkRuntimeID converts a block runtime ID found in a chunk payload to a canonical runtime ID. Unknown IDs
// become air.
func (c Codec) UnmapChunkRuntimeID(id uint32) uint32 {
	if c.chunks.unmap == nil {
		return id
	}
	if rid, ok := c.chunks.unmap(id); ok {
		return rid
	}
	return c.chunks.air
}
