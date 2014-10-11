package utp

import "errors"

type packetBuffer struct {
	buf   []*packet
	index int
	begin int
	max   int
}

func newPacketBuffer(size, front int) *packetBuffer {
	return &packetBuffer{
		buf:   make([]*packet, size),
		index: front,
		begin: 0,
		max:   front - 1,
	}
}

func (b *packetBuffer) push(p packet, id uint16) error {
	if int(id) < b.index || int(id) >= b.index+len(b.buf) {
		return errors.New("out of bounds")
	}
	index := (int(id) - b.index + b.begin) % len(b.buf)
	if b.buf[index] == nil {
		b.buf[index] = &p
	}
	if b.max < int(id) {
		b.max = int(id)
	}
	return nil
}

func (b *packetBuffer) fetch(id uint16) (packet, error) {
	index := (int(id) - b.index + b.begin) % len(b.buf)
	if index < 0 || index >= len(b.buf) {
		return packet{}, errors.New("not found")
	}
	p := b.buf[index]
	if p == nil {
		return packet{}, errors.New("not found")
	}
	b.buf[index] = nil
	return *p, nil
}

func (b *packetBuffer) compact() {
	for b.buf[b.begin] == nil && b.index <= b.max {
		b.begin = (b.begin + 1) % len(b.buf)
		b.index++
	}
}

func (b *packetBuffer) all() []packet {
	var p []packet
	for i := b.index; i <= b.max; i++ {
		index := (i - b.index + b.begin) % len(b.buf)
		if b.buf[index] != nil {
			p = append(p, *b.buf[index])
		}
	}
	return p
}

func (b *packetBuffer) first() (packet, error) {
	if b.buf[b.begin] == nil {
		return packet{}, errors.New("buffer is empty")
	}
	return *b.buf[b.begin], nil
}

func (b *packetBuffer) sequence() []packet {
	var p []packet
	for b.buf[b.begin] != nil {
		p = append(p, *b.buf[b.begin])
		b.buf[b.begin] = nil
		b.begin = (b.begin + 1) % len(b.buf)
		b.index++
	}
	return p
}

func (b *packetBuffer) space() int {
	return len(b.buf) - (b.max - b.index) - 1
}

func (b *packetBuffer) empty() bool {
	return b.space() == len(b.buf)
}
