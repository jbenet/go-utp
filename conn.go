package utp

import (
	"bytes"
	"errors"
	"io"
	"math"
	"math/rand"
	"net"
	"sync"
	"syscall"
	"time"
)

type UTPConn struct {
	conn                             *net.UDPConn
	raddr                            *net.UDPAddr
	rid, sid, seq, ack, lastAck      uint16
	rtt, rttVar, minRtt, rto, dupAck int
	diff, maxWindow                  uint32
	rdeadline, wdeadline             time.Time

	state      state
	stateMutex sync.RWMutex

	outch  chan *outgoingPacket
	sendch chan *outgoingPacket
	recvch chan *packet
	winch  chan uint32

	readch      chan []byte
	connch      chan error
	finch       chan int
	eofid       uint16
	keepalivech chan time.Duration

	readbuf   bytes.Buffer
	recvbuf   *packetBuffer
	sendbuf   *packetBuffer
	closefunc func() error
}

func Dial(n, addr string) (*UTPConn, error) {
	raddr, err := ResolveUTPAddr(n, addr)
	if err != nil {
		return nil, err
	}
	return DialUTP(n, nil, raddr)
}

func DialUTP(n string, laddr, raddr *UTPAddr) (*UTPConn, error) {
	return dial(n, laddr, raddr, 0)
}

func DialUTPTimeout(n string, laddr, raddr *UTPAddr, timeout time.Duration) (*UTPConn, error) {
	return dial(n, laddr, raddr, timeout)
}

func dial(n string, laddr, raddr *UTPAddr, timeout time.Duration) (*UTPConn, error) {
	udpnet, err := utp2udp(n)
	if err != nil {
		return nil, err
	}

	if laddr == nil {
		addr, err := net.ResolveUDPAddr(udpnet, ":0")
		if err != nil {
			return nil, err
		}
		laddr = &UTPAddr{addr: addr}
	}

	conn, err := net.ListenUDP(udpnet, laddr.addr)
	if err != nil {
		return nil, err
	}

	id := uint16(rand.Intn(math.MaxUint16))
	c := UTPConn{
		conn:      conn,
		raddr:     raddr.addr,
		rid:       id,
		sid:       id + 1,
		seq:       1,
		ack:       0,
		minRtt:    math.MaxInt64,
		diff:      0,
		maxWindow: mtu,
		rto:       1000,
		state:     state_syn_sent,

		outch:  make(chan *outgoingPacket, 10),
		sendch: make(chan *outgoingPacket, 10),
		recvch: make(chan *packet, 2),

		readch: make(chan []byte, 100),
		connch: make(chan error, 1),
		finch:  make(chan int, 1),
		winch:  make(chan uint32),

		keepalivech: make(chan time.Duration),

		sendbuf: newPacketBuffer(window_size, 1),
		closefunc: func() error {
			return conn.Close()
		},
	}

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
			c.setState(state_closed)
			return nil, err
		}
		return &c, nil
	case <-t:
		c.setState(state_closed)
		return nil, &timeoutError{}
	}
}

func (c *UTPConn) ok() bool { return c != nil && c.conn != nil }

func (c *UTPConn) Close() error {
	if !c.ok() {
		return syscall.EINVAL
	}

	state := c.getState()
	if state.active && state.exit != nil {
		state.exit(c)
		<-c.finch
	}
	return c.closefunc()
}

func (c *UTPConn) LocalAddr() net.Addr {
	return &UTPAddr{addr: c.conn.LocalAddr().(*net.UDPAddr)}
}

func (c *UTPConn) RemoteAddr() net.Addr {
	return &UTPAddr{addr: c.raddr}
}

func (c *UTPConn) Read(b []byte) (int, error) {
	if !c.ok() {
		return 0, syscall.EINVAL
	}

	state := c.getState()
	if !state.readable {
		if c.readbuf.Len() != 0 {
			return c.readbuf.Read(b)
		}
		return 0, io.EOF
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

	state := c.getState()
	if !state.writable {
		return 0, errors.New("use of closed network connection")
	}

	buf := bytes.NewBuffer(append([]byte(nil), b...))
	for {
		var payload [mss]byte
		l, err := buf.Read(payload[:])
		if err != nil {
			break
		}
		c.outch <- &outgoingPacket{st_data, nil, payload[:l]}
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
	state := c.getState()
	if state.active {
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
		len, addr, err := c.conn.ReadFromUDP(buf[:])
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

	go func() {
		var window uint32 = window_size * mtu
		for {
			if window >= mtu {
				select {
				case b := <-c.outch:
					if b != nil {
						c.sendch <- b
						window -= mtu
					} else {
						c.sendch <- nil
						return
					}
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
				c.processPacket(*p)
				lastReceived = time.Now()
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
			state := c.getState()
			if !state.active && time.Now().Sub(lastReceived) > reset_timeout {
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
			c.sendPacket(outgoingPacket{st_state, nil, nil})
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
		_, err = c.conn.WriteToUDP(bin, c.raddr)
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
		_, err = c.conn.WriteToUDP(bin, c.raddr)
		if err != nil {
			return
		}
	}
}

func currentMicrosecond() uint32 {
	return uint32(time.Now().Nanosecond() / 1000)
}

func (c *UTPConn) processPacket(p packet) {
	if p.header.t == 0 {
		c.diff = 0
	} else {
		t := currentMicrosecond()
		if t > p.header.t {
			c.diff = t - p.header.t
			if c.minRtt > int(c.diff) {
				c.minRtt = int(c.diff)
			}
		}
	}

	state := c.getState()
	if p.header.typ == st_state {
		s, err := c.sendbuf.fetch(p.header.ack)
		if err == nil {
			current := currentMicrosecond()
			if current > s.header.t {
				e := int(current-s.header.t) / 1000
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
			}
		}
		c.sendbuf.compact()
		if c.lastAck == p.header.ack {
			c.dupAck++
			if c.dupAck >= 2 {
				p, err := c.sendbuf.first()
				if err == nil {
					c.maxWindow /= 2
					if c.maxWindow < mtu {
						c.maxWindow = mtu
					}
					c.resendPacket(p)
				}
				c.dupAck = 0
			}
		} else {
			c.dupAck = 0
		}
		c.lastAck = p.header.ack
		if p.header.ack >= c.seq-1 {
			wnd := p.header.wnd
			if wnd > c.maxWindow {
				wnd = c.maxWindow
			}
			c.winch <- wnd
		}
		if state.state != nil {
			state.state(c, p)
		}
	} else if p.header.typ == st_reset {
		c.close()
	} else {
		if c.recvbuf == nil {
			return
		}
		c.sendch <- &outgoingPacket{st_state, nil, nil}
		c.recvbuf.push(p)
		for _, s := range c.recvbuf.sequence() {
			if c.ack < s.header.seq {
				state := c.getState()
				c.ack = s.header.seq
				switch s.header.typ {
				case st_data:
					if state.data != nil {
						state.data(c, s)
					}
				case st_fin:
					if state.fin != nil {
						state.fin(c, s)
					}
				case st_state:
					if state.state != nil {
						state.state(c, s)
					}
				}
			}
		}
	}
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

func (c *UTPConn) setState(s state) {
	c.stateMutex.Lock()
	defer c.stateMutex.Unlock()
	c.state = s
}

func (c *UTPConn) getState() state {
	c.stateMutex.RLock()
	defer c.stateMutex.RUnlock()
	return c.state
}

func (c *UTPConn) close() {
	state := c.getState()
	if !state.closed {
		close(c.recvch)
		close(c.outch)
		close(c.readch)
		close(c.finch)
		c.setState(state_closed)
	}
}

func (c *UTPConn) closing() {
	c.setState(state_closing)
}

type state struct {
	data     func(c *UTPConn, p packet)
	fin      func(c *UTPConn, p packet)
	state    func(c *UTPConn, p packet)
	exit     func(c *UTPConn)
	active   bool
	readable bool
	writable bool
	closed   bool
}

var state_closed state = state{
	closed: true,
}

var state_closing state = state{
	data: func(c *UTPConn, p packet) {
		c.readch <- append([]byte(nil), p.payload...)
		if c.recvbuf.empty() && c.sendbuf.empty() {
			c.close()
		}
	},
	state: func(c *UTPConn, p packet) {
		if c.recvbuf.empty() && c.sendbuf.empty() {
			c.close()
		}
	},
	readable: true,
}

var state_syn_sent state = state{
	state: func(c *UTPConn, p packet) {
		c.recvbuf = newPacketBuffer(window_size, int(p.header.seq))
		c.setState(state_connected)
		c.connch <- nil
	},
	active:   true,
	readable: true,
	writable: true,
}

var state_connected state = state{
	data: func(c *UTPConn, p packet) {
		c.readch <- append([]byte(nil), p.payload...)
	},
	fin: func(c *UTPConn, p packet) {
		if c.recvbuf.empty() && c.sendbuf.empty() {
			c.close()
		} else {
			c.closing()
		}
	},
	exit: func(c *UTPConn) {
		c.outch <- &outgoingPacket{st_fin, nil, nil}
		c.setState(state_fin_sent)
	},
	active:   true,
	readable: true,
	writable: true,
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
	readable: true,
}
