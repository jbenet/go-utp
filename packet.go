package utp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
)

type header struct {
	typ, ver, ext int
	id            uint16
	t, diff, wnd  uint32
	seq, ack      uint16
}

type packet struct {
	header  header
	payload []byte
}

type packetBase struct {
	typ, ext int
	payload  []byte
	ack      uint16
}

func (p *packet) MarshalBinary() ([]byte, error) {
	var data = []interface{}{
		// | type  | ver   |
		uint8(((byte(p.header.typ) << 4) & 0xF0) | (byte(p.header.ver) & 0xF)),
		// | extension     |
		uint8(p.header.ext),
		// | connection_id                 |
		uint16(p.header.id),
		// | timestamp_microseconds                                        |
		uint32(p.header.t),
		// | timestamp_difference_microseconds                             |
		uint32(p.header.diff),
		// | wnd_size                                                      |
		uint32(p.header.wnd),
		// | seq_nr                        |
		uint16(p.header.seq),
		// | ack_nr                        |
		uint16(p.header.ack),
	}
	buf := new(bytes.Buffer)
	for _, v := range data {
		err := binary.Write(buf, binary.BigEndian, v)
		if err != nil {
			return nil, err
		}
	}
	_, err := buf.Write(p.payload)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (p *packet) UnmarshalBinary(data []byte) error {
	buf := bytes.NewReader(data)
	var tv, ext uint8
	var d = []interface{}{
		// | type  | ver   |
		(*uint8)(&tv),
		// | extension     |
		(*uint8)(&ext),
		// | connection_id                 |
		(*uint16)(&p.header.id),
		// | timestamp_microseconds                                        |
		(*uint32)(&p.header.t),
		// | timestamp_difference_microseconds                             |
		(*uint32)(&p.header.diff),
		// | wnd_size                                                      |
		(*uint32)(&p.header.wnd),
		// | seq_nr                        |
		(*uint16)(&p.header.seq),
		// | ack_nr                        |
		(*uint16)(&p.header.ack),
	}
	for _, v := range d {
		err := binary.Read(buf, binary.BigEndian, v)
		if err != nil {
			return err
		}
	}
	p.header.typ = int((tv >> 4) & 0xF)
	p.header.ver = int(tv & 0xF)
	p.header.ext = int(ext)
	b, err := ioutil.ReadAll(buf)
	if err != nil {
		return err
	}
	p.payload = b
	return nil
}

func (p packet) String() string {
	var s string = fmt.Sprintf("[%d ", p.header.id)
	switch p.header.typ {
	case st_data:
		s += "ST_DATA"
	case st_fin:
		s += "ST_FIN"
	case st_state:
		s += "ST_STATE"
	case st_reset:
		s += "ST_RESET"
	case st_syn:
		s += "ST_SYN"
	}
	s += fmt.Sprintf(" seq:%d ack:%d len:%d", p.header.seq, p.header.ack, len(p.payload))
	s += "]"
	return s
}
