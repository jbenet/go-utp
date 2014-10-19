package utp

import (
	"errors"
	"math"
)

type packetBuffer struct {
	root  *packetBufferNode
	size  int
	begin int
}

type packetBufferNode struct {
	p    *packet
	next *packetBufferNode
}

func newPacketBuffer(size, begin int) *packetBuffer {
	return &packetBuffer{
		size:  size,
		begin: begin,
	}
}

func (b *packetBuffer) push(p packet) error {
	if int(p.header.seq) > b.begin+b.size-1 {
		return errors.New("out of bounds")
	} else if int(p.header.seq) < b.begin {
		if int(p.header.seq)+math.MaxUint16 > b.begin+b.size-1 {
			return errors.New("out of bounds")
		}
	}
	if b.root == nil {
		b.root = &packetBufferNode{}
	}
	n := b.root
	i := b.begin
	for {
		if i == int(p.header.seq) {
			n.p = &p
			return nil
		} else if n.next == nil {
			n.next = &packetBufferNode{}
		}
		n = n.next
		i = (i + 1) % (math.MaxUint16 + 1)
	}
	return nil
}

func (b *packetBuffer) fetch(id uint16) (packet, error) {
	for p := b.root; p != nil; p = p.next {
		if p.p != nil && p.p.header.seq == id {
			r := *p.p
			p.p = nil
			return r, nil
		}
	}
	return packet{}, errors.New("not found")
}

func (b *packetBuffer) compact() {
	for b.root != nil && b.root.p == nil {
		b.root = b.root.next
		b.begin = (b.begin + 1) % (math.MaxUint16 + 1)
	}
}

func (b *packetBuffer) all() []packet {
	var a []packet
	for p := b.root; p != nil; p = p.next {
		if p.p != nil {
			a = append(a, *p.p)
		}
	}
	return a
}

func (b *packetBuffer) first() (packet, error) {
	if b.root == nil || b.root.p == nil {
		return packet{}, errors.New("buffer is empty")
	}
	return *b.root.p, nil
}

func (b *packetBuffer) sequence() []packet {
	var a []packet
	for ; b.root != nil && b.root.p != nil; b.root = b.root.next {
		a = append(a, *b.root.p)
		b.begin = (b.begin + 1) % (math.MaxUint16 + 1)
	}
	return a
}

func (b *packetBuffer) space() int {
	s := b.size
	for p := b.root; p != nil; p = p.next {
		s--
	}
	return s
}

func (b *packetBuffer) empty() bool {
	return b.root == nil
}
