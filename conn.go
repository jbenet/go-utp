package utp

import (
	"bytes"
	"errors"
	"io"
	"math"
	"math/rand"
	"net"
	"syscall"
	"time"
)

type UTPConn struct {
	Conn                             net.PacketConn
	raddr                            net.Addr
	rid, sid, seq, ack, lastAck      uint16
	rtt, rttVar, minRtt, rto, dupAck int64
	diff, maxWindow                  uint32
	rdeadline, wdeadline             time.Time

	state state

	exitch   chan int
	outch    chan *outgoingPacket
	outchch  chan chan *outgoingPacket
	sendch   chan *outgoingPacket
	recvch   chan *packet
	winch    chan uint32
	quitch   chan int
	activech chan bool

	readch      chan []byte
	readchch    chan chan []byte
	connch      chan error
	finch       chan int
	closech     chan<- uint16
	eofid       uint16
	keepalivech chan time.Duration

	readbuf bytes.Buffer
	recvbuf *packetBuffer
	sendbuf *packetBuffer
}

func dial(n string, laddr, raddr *UTPAddr, timeout time.Duration) (*UTPConn, error) {
	udpnet, err := utp2udp(n)
	if err != nil {
		return nil, err
	}

	// TODO extract
	if laddr == nil {
		addr, err := net.ResolveUDPAddr(udpnet, ":0")
		if err != nil {
			return nil, err
		}
		laddr = &UTPAddr{addr: addr}
	}

	conn, err := net.ListenPacket(udpnet, laddr.addr.String())
	if err != nil {
		return nil, err
	}

	id := uint16(rand.Intn(math.MaxUint16))

	c := newUTPConn()
	c.Conn = conn
	c.raddr = raddr.addr
	c.rid = id
	c.sid = id + 1
	c.seq = 1
	c.state = state_syn_sent
	c.sendbuf = newPacketBuffer(window_size, 1)

	go c.recv()
	go c.loop()

	c.sendch <- &outgoingPacket{st_syn, nil, nil}

	var t <-chan time.Time
	if timeout != 0 {
		t = time.After(timeout)
	}

	select {
	case err := <-c.connch:
		if err != nil {
			c.closed()
			return nil, err
		}
		ulog.Printf(1, "Conn(%v): Connected", c.LocalAddr())
		return c, nil
	case <-t:
		c.closed()
		return nil, &timeoutError{}
	}
}

func newUTPConn() *UTPConn {
	return &UTPConn{
		minRtt:    math.MaxInt64,
		maxWindow: mtu,
		rto:       1000,

		exitch:   make(chan int),
		outchch:  make(chan chan *outgoingPacket),
		sendch:   make(chan *outgoingPacket, 10),
		recvch:   make(chan *packet, 2),
		winch:    make(chan uint32, 2),
		quitch:   make(chan int),
		activech: make(chan bool),

		readchch: make(chan chan []byte),
		connch:   make(chan error, 1),
		finch:    make(chan int, 1),

		keepalivech: make(chan time.Duration),
	}
}

func (c *UTPConn) ok() bool { return c != nil && c.Conn != nil }

func (c *UTPConn) Close() error {
	if !c.ok() {
		return syscall.EINVAL
	}

	if <-c.activech {
		c.quitch <- 0
		ulog.Printf(2, "Conn(%v): Wait for close", c.LocalAddr())
		<-c.finch
	}
	return nil
}

func (c *UTPConn) LocalAddr() net.Addr {
	return &UTPAddr{addr: c.Conn.LocalAddr().(*net.UDPAddr)}
}

func (c *UTPConn) RemoteAddr() net.Addr {
	return &UTPAddr{addr: c.raddr}
}

func (c *UTPConn) Read(b []byte) (int, error) {
	if !c.ok() {
		return 0, syscall.EINVAL
	}

	if c.readbuf.Len() == 0 {
		var timeout <-chan time.Time
		if !c.rdeadline.IsZero() {
			timeout = time.After(c.rdeadline.Sub(time.Now()))
		}

		select {
		case b := <-c.readch:
			if b == nil {
				return 0, io.EOF
			}
			_, err := c.readbuf.Write(b)
			if err != nil {
				return 0, err
			}
		case <-timeout:
			return 0, &timeoutError{}
		}
	}

	select {
	case b := <-c.readch:
		_, err := c.readbuf.Write(b)
		if err != nil {
			return 0, err
		}
	default:
	}

	return c.readbuf.Read(b)
}

func (c *UTPConn) Write(b []byte) (int, error) {
	if !c.ok() {
		return 0, syscall.EINVAL
	}

	var wrote int
	buf := bytes.NewBuffer(append([]byte(nil), b...))
	for {
		var payload [mss]byte
		l, err := buf.Read(payload[:])
		if err != nil {
			break
		}
		if outch, ok := <-c.outchch; ok {
			outch <- &outgoingPacket{st_data, nil, payload[:l]}
		} else {
			return 0, errors.New("use of closed network connection")
		}
		wrote += l
		ulog.Printf(4, "Conn(%v): Write %d/%d bytes", c.LocalAddr(), wrote, len(b))
		if l < mss {
			break
		}
	}

	return len(b), nil
}

func (c *UTPConn) SetDeadline(t time.Time) error {
	if !c.ok() {
		return syscall.EINVAL
	}
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	if err := c.SetWriteDeadline(t); err != nil {
		return err
	}
	return nil
}

func (c *UTPConn) SetReadDeadline(t time.Time) error {
	if !c.ok() {
		return syscall.EINVAL
	}
	c.rdeadline = t
	return nil
}

func (c *UTPConn) SetWriteDeadline(t time.Time) error {
	if !c.ok() {
		return syscall.EINVAL
	}
	c.wdeadline = t
	return nil
}

func (c *UTPConn) SetKeepAlive(d time.Duration) error {
	if !c.ok() {
		return syscall.EINVAL
	}
	if <-c.activech {
		c.keepalivech <- d
	}
	return nil
}

func readPacket(data []byte) (packet, error) {
	var p packet
	err := p.UnmarshalBinary(data)
	if err != nil {
		return p, err
	}
	if p.header.ver != version {
		return p, errors.New("unsupported header version")
	}
	return p, nil
}

func (c *UTPConn) recv() {
	for {
		var buf [mtu]byte
		len, addr, err := c.Conn.ReadFrom(buf[:])
		if err != nil {
			return
		}
		if addr.String() != c.raddr.String() {
			continue
		}
		p, err := readPacket(buf[:len])
		if err == nil {
			c.recvch <- &p
		}
	}
}

func (c *UTPConn) loop() {
	var recvExit, sendExit bool
	var lastReceived time.Time
	var keepalive <-chan time.Time

	c.outch = make(chan *outgoingPacket, 10)
	c.readch = make(chan []byte, 100)

	go func() {
		for {
			select {
			case c.outchch <- c.outch:
			case c.readchch <- c.readch:
			case c.activech <- true:
			case <-c.exitch:
				close(c.outchch)
				close(c.outch)
				close(c.readchch)
				close(c.readch)
				close(c.activech)
				return
			}
		}
	}()

	go func() {
		var window uint32 = window_size * mtu
		for {
			if window >= mtu {
				select {
				case b := <-c.outch:
					if b == nil {
						c.sendch <- nil
						return
					}
					c.sendch <- b
					window -= mtu
				case w := <-c.winch:
					window = w
				}
			} else {
				window = <-c.winch
			}
		}
	}()

	for {
		select {
		case p := <-c.recvch:
			if p != nil {
				ack := c.processPacket(*p)
				lastReceived = time.Now()
				if ack {
					c.sendPacket(outgoingPacket{st_state, nil, nil})
				}
			} else {
				recvExit = true
			}

		case b := <-c.sendch:
			if b != nil {
				c.sendPacket(*b)
			} else {
				sendExit = true
			}

		case <-time.After(time.Duration(c.rto) * time.Millisecond):
			if !c.state.active && time.Now().Sub(lastReceived) > reset_timeout {
				ulog.Printf(2, "Conn(%v): Connection timed out", c.LocalAddr())
				c.sendPacket(outgoingPacket{st_reset, nil, nil})
				c.close()
			} else {
				p, err := c.sendbuf.first()
				if err == nil {
					c.maxWindow /= 2
					if c.maxWindow < mtu {
						c.maxWindow = mtu
					}
					c.resendPacket(p)
				}
			}
		case d := <-c.keepalivech:
			if d <= 0 {
				keepalive = nil
			} else {
				keepalive = time.Tick(d)
			}
		case <-keepalive:
			ulog.Printf(2, "Conn(%v): Send keepalive", c.LocalAddr())
			c.sendPacket(outgoingPacket{st_state, nil, nil})

		case <-c.quitch:
			if c.state.exit != nil {
				c.state.exit(c)
			}
		}
		if recvExit && sendExit {
			return
		}
	}
}

func (c *UTPConn) sendPacket(b outgoingPacket) {
	p := c.makePacket(b)
	bin, err := p.MarshalBinary()
	if err == nil {
		ulog.Printf(3, "SEND %v -> %v: %v", c.Conn.LocalAddr(), c.raddr, p.String())
		_, err = c.Conn.WriteTo(bin, c.raddr)
		if err != nil {
			return
		}
		if b.typ != st_state {
			c.sendbuf.push(*p)
		}
	}
}

func (c *UTPConn) resendPacket(p packet) {
	bin, err := p.MarshalBinary()
	if err == nil {
		ulog.Printf(3, "RESEND %v -> %v: %v", c.Conn.LocalAddr(), c.raddr, p.String())
		_, err = c.Conn.WriteTo(bin, c.raddr)
		if err != nil {
			return
		}
	}
}

func currentMicrosecond() uint32 {
	return uint32(time.Now().Nanosecond() / 1000)
}

func (c *UTPConn) processPacket(p packet) bool {
	var ack bool

	if p.header.t == 0 {
		c.diff = 0
	} else {
		t := currentMicrosecond()
		if t > p.header.t {
			c.diff = t - p.header.t
			if c.minRtt > int64(c.diff) {
				c.minRtt = int64(c.diff)
			}
		}
	}

	ulog.Printf(3, "RECV %v -> %v: %v", c.raddr, c.Conn.LocalAddr(), p.String())

	if p.header.typ == st_state {
		s, err := c.sendbuf.fetch(p.header.ack)
		if err == nil {
			current := currentMicrosecond()
			if current > s.header.t {
				e := int64(current-s.header.t) / 1000
				if c.rtt == 0 {
					c.rtt = e
					c.rttVar = e / 2
				} else {
					d := c.rtt - e
					if d < 0 {
						d = -d
					}
					c.rttVar += (d - c.rttVar) / 4
					c.rtt = c.rtt - c.rtt/8 + e/8
				}
				c.rto = c.rtt + c.rttVar*4
				if c.rto < 500 {
					c.rto = 500
				}
			}

			if c.diff != 0 {
				ourDelay := float64(c.diff)
				offTarget := 100000.0 - ourDelay
				windowFactor := float64(mtu) / float64(c.maxWindow)
				delayFactor := offTarget / 100000.0
				gain := 3000.0 * delayFactor * windowFactor
				c.maxWindow = uint32(int(c.maxWindow) + int(gain))
				if c.maxWindow < mtu {
					c.maxWindow = mtu
				}
				ulog.Printf(4, "Conn(%v): Update maxWindow: %d", c.LocalAddr(), c.maxWindow)
			}
		}
		c.sendbuf.compact()
		if c.lastAck == p.header.ack {
			c.dupAck++
			if c.dupAck >= 2 {
				ulog.Printf(3, "Conn(%v): Receive 3 duplicated acks: %d", c.LocalAddr(), p.header.ack)
				p, err := c.sendbuf.first()
				if err == nil {
					c.maxWindow /= 2
					if c.maxWindow < mtu {
						c.maxWindow = mtu
					}
					ulog.Printf(4, "Conn(%v): Update maxWindow: %d", c.LocalAddr(), c.maxWindow)
					c.resendPacket(p)
				}
				c.dupAck = 0
			}
		} else {
			c.dupAck = 0
		}
		c.lastAck = p.header.ack
		if p.header.ack == c.seq-1 {
			wnd := p.header.wnd
			if wnd > c.maxWindow {
				wnd = c.maxWindow
			}
			ulog.Printf(4, "Conn(%v): Reset window: %d", c.LocalAddr(), wnd)
			go func() {
				c.winch <- wnd
			}()
		}
		if c.state.state != nil {
			c.state.state(c, p)
		}
	} else if p.header.typ == st_reset {
		c.close()
	} else {
		if c.recvbuf == nil {
			return false
		}
		ack = true
		c.recvbuf.push(p)
		for _, s := range c.recvbuf.sequence() {
			c.ack = s.header.seq
			switch s.header.typ {
			case st_data:
				if c.state.data != nil {
					c.state.data(c, s)
				}
			case st_fin:
				if c.state.fin != nil {
					c.state.fin(c, s)
				}
			case st_state:
				if c.state.state != nil {
					c.state.state(c, s)
				}
			}
		}
	}
	return ack
}

func (c *UTPConn) makePacket(b outgoingPacket) *packet {
	wnd := window_size * mtu
	if c.recvbuf != nil {
		wnd = c.recvbuf.space() * mtu
	}
	id := c.sid
	if b.typ == st_syn {
		id = c.rid
	}
	h := header{
		typ:  b.typ,
		ver:  version,
		id:   id,
		t:    currentMicrosecond(),
		diff: c.diff,
		wnd:  uint32(wnd),
		seq:  c.seq,
		ack:  c.ack,
	}
	if b.typ == st_fin {
		c.eofid = c.seq
	}
	if !(b.typ == st_state && len(b.payload) == 0) {
		c.seq++
	}
	return &packet{header: h, payload: b.payload}
}

func (c *UTPConn) close() {
	if !c.state.closed {
		c.recvch <- nil
		close(c.exitch)
		close(c.finch)
		c.closed()

		// Accepted connection
		if c.closech != nil {
			c.closech <- c.sid
		} else {
			c.Conn.Close()
		}

		ulog.Printf(1, "Conn(%v): Closed", c.LocalAddr())
	}
}

func (c *UTPConn) closed() {
	ulog.Printf(2, "Conn(%v): Change state: CLOSED", c.LocalAddr())
	c.state = state_closed
}

func (c *UTPConn) closing() {
	ulog.Printf(2, "Conn(%v): Change state: CLOSING", c.LocalAddr())
	c.state = state_closing
}

func (c *UTPConn) syn_sent() {
	ulog.Printf(2, "Conn(%v): Change state: SYN_SENT", c.LocalAddr())
	c.state = state_syn_sent
}

func (c *UTPConn) connected() {
	ulog.Printf(2, "Conn(%v): Change state: CONNECTED", c.LocalAddr())
	c.state = state_connected
}

func (c *UTPConn) fin_sent() {
	ulog.Printf(2, "Conn(%v): Change state: FIN_SENT", c.LocalAddr())
	c.state = state_fin_sent
}

type state struct {
	data   func(c *UTPConn, p packet)
	fin    func(c *UTPConn, p packet)
	state  func(c *UTPConn, p packet)
	exit   func(c *UTPConn)
	active bool
	closed bool
}

var state_closed state = state{
	closed: true,
}

var state_closing state = state{
	data: func(c *UTPConn, p packet) {
		if readch, ok := <-c.readchch; ok {
			readch <- append([]byte(nil), p.payload...)
		}
		if c.recvbuf.empty() && c.sendbuf.empty() {
			c.close()
		}
	},
	state: func(c *UTPConn, p packet) {
		if c.recvbuf.empty() && c.sendbuf.empty() {
			c.close()
		}
	},
}

var state_syn_sent state = state{
	state: func(c *UTPConn, p packet) {
		c.recvbuf = newPacketBuffer(window_size, int(p.header.seq))
		c.connected()
		c.connch <- nil
	},
	active: true,
}

var state_connected state = state{
	data: func(c *UTPConn, p packet) {
		if readch, ok := <-c.readchch; ok {
			readch <- append([]byte(nil), p.payload...)
		}
	},
	fin: func(c *UTPConn, p packet) {
		if c.recvbuf.empty() && c.sendbuf.empty() {
			c.close()
		} else {
			c.closing()
		}
	},
	exit: func(c *UTPConn) {
		if outch, ok := <-c.outchch; ok {
			go func() {
				outch <- &outgoingPacket{st_fin, nil, nil}
			}()
		}
		c.fin_sent()
	},
	active: true,
}

var state_fin_sent state = state{
	state: func(c *UTPConn, p packet) {
		if p.header.ack == c.eofid {
			if c.recvbuf.empty() && c.sendbuf.empty() {
				c.close()
			} else {
				c.closing()
			}
		}
	},
}
