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
		if p.p != nil {
			if p.p.header.seq < id {
				p.p = nil
			} else if p.p.header.seq == id {
				r := *p.p
				p.p = nil
				return r, nil
			}
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

func (b *packetBuffer) fetchSequence() []packet {
	var a []packet
	for ; b.root != nil && b.root.p != nil; b.root = b.root.next {
		a = append(a, *b.root.p)
		b.begin = (b.begin + 1) % (math.MaxUint16 + 1)
	}
	return a
}

func (b *packetBuffer) sequence() []packet {
	var a []packet
	n := b.root
	for ; n != nil && n.p != nil; n = n.next {
		a = append(a, *n.p)
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

func (b *packetBuffer) generateSelectiveACK() []byte {
	if b.empty() {
		return nil
	}

	var ack []byte
	var bit uint
	var octet byte
	for p := b.root.next; p != nil; p = p.next {
		if p.p != nil {
			octet |= (1 << bit)
		}
		bit++
		if bit == 8 {
			ack = append(ack, octet)
			bit = 0
			octet = 0
		}
	}

	if bit != 0 {
		ack = append(ack, octet)
	}

	for len(ack) > 0 && ack[len(ack)-1] == 0 {
		ack = ack[:len(ack)-1]
	}

	return ack
}

func (b *packetBuffer) processSelectiveACK(ack []byte) {
	if b.empty() {
		return
	}

	p := b.root.next
	if p == nil {
		return
	}

	for _, a := range ack {
		for i := 0; i < 8; i++ {
			acked := (a & 1) != 0
			a >>= 1
			if acked {
				p.p = nil
			}
			p = p.next
			if p == nil {
				return
			}
		}
	}
}
