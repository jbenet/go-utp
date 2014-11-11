package utp

import (
	"bytes"
	"io"
	"io/ioutil"
	"math"
	"math/rand"
	"net"
	"reflect"
	"testing"
	"time"
)

func init() {
	rand.Seed(time.Now().Unix())
}

func TestReadWrite(t *testing.T) {
	ln, err := Listen("utp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	raddr, err := ResolveUTPAddr("utp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	c, err := DialUTPTimeout("utp", nil, raddr, 1000*time.Millisecond)
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
	ln.Close()

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
	ln, err := Listen("utp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	raddr, err := ResolveUTPAddr("utp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	c, err := DialUTPTimeout("utp", nil, raddr, 1000*time.Millisecond)
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
	ln.Close()

	var payload [10485760]byte
	for i := range payload {
		payload[i] = byte(rand.Int())
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

func TestDialer(t *testing.T) {
	ln, err := Listen("utp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	d := Dialer{}
	c, err := d.Dial("utp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
}

func TestDialerAddrs(t *testing.T) {
	ln, err := Listen("utp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	laddr, err := ResolveUTPAddr("utp", "127.0.0.1:45678")
	if err != nil {
		t.Fatal(err)
	}

	d := Dialer{LocalAddr: laddr}
	c1, err := d.Dial("utp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()

	c2, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()

	eq := func(a, b net.Addr) bool {
		return a.String() == b.String()
	}

	if !eq(d.LocalAddr, c2.RemoteAddr()) {
		t.Fatal("dialer.LocalAddr not equal to c2.RemoteAddr ")
	}
	if !eq(c1.LocalAddr(), c2.RemoteAddr()) {
		t.Fatal("c1.LocalAddr not equal to c2.RemoteAddr ")
	}
	if !eq(c2.LocalAddr(), c1.RemoteAddr()) {
		t.Fatal("c2.LocalAddr not equal to c1.RemoteAddr ")
	}
}

func TestDialerTimeout(t *testing.T) {
	timeout := time.Millisecond * 200
	d := Dialer{Timeout: timeout}
	done := make(chan struct{})

	go func() {
		_, err := d.Dial("utp", "127.0.0.1:34567")
		if err == nil {
			t.Fatal("should not connect")
		}
		done <- struct{}{}
	}()

	select {
	case <-time.After(timeout * 2):
		t.Fatal("should have ended already")
	case <-done:
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
