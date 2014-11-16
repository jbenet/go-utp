package utp

import (
	"errors"
	"math"
	"math/rand"
	"net"
	"syscall"
	"time"
)

type UTPListener struct {
	Conn     net.PacketConn
	conns    map[uint16]*UTPConn
	accept   chan (*UTPConn)
	err      chan (error)
	lasterr  error
	deadline time.Time
	closech  chan int
	connch   chan uint16
	closed   bool
}

func Listen(n, laddr string) (*UTPListener, error) {
	addr, err := ResolveUTPAddr(n, laddr)
	if err != nil {
		return nil, err
	}
	return ListenUTP(n, addr)
}

func ListenUTP(n string, laddr *UTPAddr) (*UTPListener, error) {
	udpnet, err := utp2udp(n)
	if err != nil {
		return nil, err
	}
	conn, err := listenPacket(udpnet, laddr.Addr.String())
	if err != nil {
		return nil, err
	}

	l := UTPListener{
		Conn:    conn,
		conns:   make(map[uint16]*UTPConn),
		accept:  make(chan (*UTPConn), 10),
		err:     make(chan (error)),
		closech: make(chan int),
		connch:  make(chan uint16),
		lasterr: nil,
	}

	l.listen()
	return &l, nil
}

type incoming struct {
	p    packet
	addr net.Addr
}

func (l *UTPListener) listen() {
	inch := make(chan incoming)

	// reads udp packets
	go func() {
		for {
			var buf [mtu]byte
			len, addr, err := l.Conn.ReadFrom(buf[:])
			if err != nil {
				l.err <- err
				return
			}
			p, err := readPacket(buf[:len])
			if err == nil {
				inch <- incoming{p, addr}
			}
		}
	}()

	go func() {
		for {
			select {
			case i := <-inch:
				l.processPacket(i.p, i.addr)
			case <-l.closech:
				ulog.Printf(2, "Listener(%v): Stop litening", l.Conn.LocalAddr())
				close(l.accept)
				l.closed = true
			case id := <-l.connch:
				if _, ok := l.conns[id]; !ok {
					delete(l.conns, id+1)
					ulog.Printf(2, "Listener(%v): Connection closed #%d (alive: %d)", l.Conn.LocalAddr(), id, len(l.conns))
					if l.closed && len(l.conns) == 0 {
						ulog.Printf(2, "Listener(%v): All accepted connections are closed", l.Conn.LocalAddr())
						l.Conn.Close()
						ulog.Printf(1, "Listener(%v): Closed", l.Conn.LocalAddr())
					}
				}
			}
		}
	}()

	ulog.Printf(1, "Listener(%v): Start listening", l.Conn.LocalAddr())
}

func listenPacket(n, addr string) (net.PacketConn, error) {
	if n == "mem" {
		return nil, errors.New("TODO implement in-memory packet connection")
	}
	return net.ListenPacket(n, addr)
}

func (l *UTPListener) processPacket(p packet, addr net.Addr) {
	switch p.header.typ {
	case st_data, st_fin, st_state, st_reset:
		if c, ok := l.conns[p.header.id]; ok {
			select {
			case c.recvch <- p:
			case <-c.recvchch:
			}
		}
	case st_syn:
		if l.closed {
			return
		}
		sid := p.header.id + 1
		if _, ok := l.conns[p.header.id]; !ok {
			seq := rand.Intn(math.MaxUint16)

			c := newUTPConn()
			c.Conn = l.Conn
			c.raddr = addr
			c.rid = p.header.id + 1
			c.sid = p.header.id
			c.seq = uint16(seq)
			c.ack = p.header.seq
			c.diff = currentMicrosecond() - p.header.t
			c.state = state_connected
			c.closech = l.connch
			c.recvbuf = newPacketBuffer(window_size, int(p.header.seq))
			c.sendbuf = newPacketBuffer(window_size, seq)

			go c.loop()
			select {
			case c.recvch <- p:
			case <-c.recvchch:
			}

			l.conns[sid] = c
			ulog.Printf(2, "Listener(%v): New incoming connection #%d from %v (alive: %d)", l.Conn.LocalAddr(), sid, addr, len(l.conns))

			l.accept <- c
		}
	}
}

func (l *UTPListener) Accept() (net.Conn, error) {
	return l.AcceptUTP()
}

func (l *UTPListener) AcceptUTP() (*UTPConn, error) {
	if l == nil || l.Conn == nil {
		return nil, syscall.EINVAL
	}
	if l.lasterr != nil {
		return nil, l.lasterr
	}
	var timeout <-chan time.Time
	if !l.deadline.IsZero() {
		timeout = time.After(l.deadline.Sub(time.Now()))
	}
	select {
	case conn := <-l.accept:
		if conn == nil {
			return nil, errors.New("use of closed network connection")
		}
		return conn, nil
	case err := <-l.err:
		l.lasterr = err
		return nil, err
	case <-timeout:
		return nil, &timeoutError{}
	}
}

func (l *UTPListener) Addr() net.Addr {
	return &UTPAddr{Addr: l.Conn.LocalAddr()}
}

func (l *UTPListener) Close() error {
	if l == nil || l.Conn == nil {
		return syscall.EINVAL
	}
	l.closech <- 0
	return nil
}

func (l *UTPListener) SetDeadline(t time.Time) error {
	if l == nil || l.Conn == nil {
		return syscall.EINVAL
	}
	l.deadline = t
	return nil
}
