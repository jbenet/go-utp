package utp

import (
	"bytes"
	"crypto/rand"
	"io"
	"io/ioutil"
	"math"
	mrand "math/rand"
	"net"
	"reflect"
	"testing"
	"time"
)

func init() {
	mrand.Seed(time.Now().Unix())
}

func TestReadWrite(t *testing.T) {
	laddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:10000")
	if err != nil {
		t.Fatal(err)
	}
	saddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:11000")
	if err != nil {
		t.Fatal(err)
	}
	caddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:12000")
	if err != nil {
		t.Fatal(err)
	}

	proxy, err := newProxy(laddr, saddr, caddr, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.close()

	usaddr, err := ResolveUTPAddr("utp", "127.0.0.1:11000")
	if err != nil {
		t.Fatal(err)
	}

	ln, err := ListenUTP("utp", usaddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	upaddr, err := ResolveUTPAddr("utp", "127.0.0.1:10000")
	if err != nil {
		t.Fatal(err)
	}
	ucaddr, err := ResolveUTPAddr("utp", "127.0.0.1:12000")
	if err != nil {
		t.Fatal(err)
	}

	c, err := DialUTPTimeout("utp", ucaddr, upaddr, 1000*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = ln.SetDeadline(time.Now().Add(1000 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	s, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}

	payload := []byte("Hello!")
	_, err = c.Write(payload)
	if err != nil {
		t.Fatal(err)
	}

	err = s.SetDeadline(time.Now().Add(1000 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	var buf [256]byte
	l, err := s.Read(buf[:])
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(payload, buf[:l]) {
		t.Errorf("expected payload of %v; got %v", payload, buf[:l])
	}

	payload2 := []byte("World!")
	_, err = s.Write(payload2)
	if err != nil {
		t.Fatal(err)
	}

	err = c.SetDeadline(time.Now().Add(1000 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	l, err = c.Read(buf[:])
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(payload2, buf[:l]) {
		t.Errorf("expected payload of %v; got %v", payload2, buf[:l])
	}
}

func TestLongReadWrite(t *testing.T) {
	laddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:20000")
	if err != nil {
		t.Fatal(err)
	}
	saddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:21000")
	if err != nil {
		t.Fatal(err)
	}
	caddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:22000")
	if err != nil {
		t.Fatal(err)
	}

	proxy, err := newProxy(laddr, saddr, caddr, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.close()

	usaddr, err := ResolveUTPAddr("utp", "127.0.0.1:21000")
	if err != nil {
		t.Fatal(err)
	}

	ln, err := ListenUTP("utp", usaddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	upaddr, err := ResolveUTPAddr("utp", "127.0.0.1:20000")
	if err != nil {
		t.Fatal(err)
	}
	ucaddr, err := ResolveUTPAddr("utp", "127.0.0.1:22000")
	if err != nil {
		t.Fatal(err)
	}

	c, err := DialUTPTimeout("utp", ucaddr, upaddr, 1000*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = ln.SetDeadline(time.Now().Add(1000 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	s, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var payload [1048576]byte
	_, err = rand.Read(payload[:])
	if err != nil {
		t.Fatal(err)
	}

	err = s.SetDeadline(time.Now().Add(10000 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	rch := make(chan []byte)
	ech := make(chan error, 2)

	go func() {
		defer c.Close()
		_, err := c.Write(payload[:])
		if err != nil {
			ech <- err
		}
	}()

	go func() {
		b, err := ioutil.ReadAll(s)
		if err != nil {
			ech <- err
			rch <- nil
		} else {
			ech <- nil
			rch <- b
		}
	}()

	err = <-ech
	if err != nil {
		t.Fatal(err)
	}

	r := <-rch
	if r == nil {
		return
	}

	if !bytes.Equal(r, payload[:]) {
		t.Errorf("expected payload of %d; got %d", len(payload[:]), len(r))
	}
}

func TestAccept(t *testing.T) {
	ln, err := Listen("utp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	c, err := DialUTPTimeout("utp", nil, ln.Addr().(*UTPAddr), 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = ln.SetDeadline(time.Now().Add(100 * time.Millisecond))
	_, err = ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
}

func TestAcceptDeadline(t *testing.T) {
	ln, err := Listen("utp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	err = ln.SetDeadline(time.Now().Add(time.Millisecond))
	_, err = ln.Accept()
	if err == nil {
		t.Fatal("Accept should failed")
	}
}

func TestAcceptClosedListener(t *testing.T) {
	ln, err := Listen("utp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	err = ln.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, err = ln.Accept()
	if err == nil {
		t.Fatal("Accept should failed")
	}
	_, err = ln.Accept()
	if err == nil {
		t.Fatal("Accept should failed")
	}
}

func TestPacketBinary(t *testing.T) {
	h := header{
		typ:  st_fin,
		ver:  version,
		id:   100,
		t:    50000,
		diff: 10000,
		wnd:  65535,
		seq:  100,
		ack:  200,
	}

	e := []extension{
		extension{
			typ:     ext_selective_ack,
			payload: []byte{0, 1, 0, 1},
		},
		extension{
			typ:     ext_selective_ack,
			payload: []byte{100, 0, 200, 0},
		},
	}

	p := packet{
		header:  h,
		ext:     e,
		payload: []byte("abcdefg"),
	}

	b, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	p2 := packet{}
	err = p2.UnmarshalBinary(b)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(p, p2) {
		t.Errorf("expected packet of %v; got %v", p, p2)
	}
}

func TestUnmarshalShortPacket(t *testing.T) {
	b := make([]byte, 18)
	p := packet{}
	err := p.UnmarshalBinary(b)

	if err == nil {
		t.Fatal("UnmarshalBinary should fail")
	} else if err != io.EOF {
		t.Fatal(err)
	}
}

func TestPacketBuffer(t *testing.T) {
	size := 12
	b := newPacketBuffer(12, 1)

	if b.space() != size {
		t.Errorf("expected space == %d; got %d", size, b.space())
	}

	for i := 1; i <= size; i++ {
		b.push(packet{header: header{seq: uint16(i)}})
	}

	if b.space() != 0 {
		t.Errorf("expected space == 0; got %d", b.space())
	}

	err := b.push(packet{header: header{seq: 15}})
	if err == nil {
		t.Fatal("push should fail")
	}

	all := b.all()
	if len(all) != size {
		t.Errorf("expected %d packets sequence; got %d", size, len(all))
	}

	_, err = b.fetch(6)
	if err != nil {
		t.Fatal(err)
	}

	b.compact()

	err = b.push(packet{header: header{seq: 15}})
	if err != nil {
		t.Fatal(err)
	}

	err = b.push(packet{header: header{seq: 17}})
	if err != nil {
		t.Fatal(err)
	}

	for i := 7; i <= size; i++ {
		_, err := b.fetch(uint16(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	all = b.all()
	if len(all) != 2 {
		t.Errorf("expected 2 packets sequence; got %d", len(all))
	}

	b.compact()
	if b.space() != 9 {
		t.Errorf("expected space == 9; got %d", b.space())
	}
}

func TestPacketBufferBoundary(t *testing.T) {
	begin := math.MaxUint16 - 3
	b := newPacketBuffer(12, begin)
	for i := begin; i != 5; i = (i + 1) % (math.MaxUint16 + 1) {
		err := b.push(packet{header: header{seq: uint16(i)}})
		if err != nil {
			t.Fatal(err)
		}
	}
}

type proxy struct {
	ln *net.UDPConn
}

func newProxy(laddr, saddr, caddr *net.UDPAddr, rate float32) (*proxy, error) {
	ln, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			var buf [mtu]byte
			len, addr, err := ln.ReadFromUDP(buf[:])
			if err != nil {
				return
			}

			var p packet
			p.UnmarshalBinary(buf[:len])
			if mrand.Float32() > rate {
				continue
			}

			if addr.String() == saddr.String() {
				_, err := ln.WriteToUDP(buf[:len], caddr)
				if err != nil {
					return
				}
			} else if addr.String() == caddr.String() {
				_, err := ln.WriteToUDP(buf[:len], saddr)
				if err != nil {
					return
				}
			}
		}
	}()
	return &proxy{ln: ln}, nil
}

func (p *proxy) close() {
	p.ln.Close()
}
