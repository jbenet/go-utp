package utp

import (
	"math"
	"math/rand"
	"net"
	"sync"
	"syscall"
	"time"
)

type UTPListener struct {
	conn     *net.UDPConn
	conns    *connMap
	accept   chan (*UTPConn)
	err      chan (error)
	lasterr  error
	deadline time.Time
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
	conn, err := net.ListenUDP(udpnet, laddr.addr)
	if err != nil {
		return nil, err
	}

	l := UTPListener{
		conn:    conn,
		conns:   &connMap{m: make(map[uint16]*UTPConn)},
		accept:  make(chan (*UTPConn)),
		err:     make(chan (error)),
		lasterr: nil,
	}

	go l.listen()
	return &l, nil
}

func (l *UTPListener) listen() {
	for {
		var buf [mtu]byte
		len, addr, err := l.conn.ReadFromUDP(buf[:])
		if err != nil {
			l.err <- err
			return
		}
		p, err := readPacket(buf[:len])
		if err == nil {
			l.processPacket(p, addr)
		}
	}
}

func (l *UTPListener) processPacket(p packet, addr *net.UDPAddr) {
	switch p.header.typ {
	case st_data, st_fin, st_state, st_reset:
		if c := l.conns.get(p.header.id); c != nil {
			state := c.getState()
			if !state.closed {
				c.recvch <- &p
			}
		}
	case st_syn:
		sid := p.header.id + 1
		if !l.conns.contains(sid) {
			seq := rand.Intn(math.MaxUint16)
			c := UTPConn{
				conn:      l.conn,
				raddr:     addr,
				rid:       p.header.id + 1,
				sid:       p.header.id,
				seq:       uint16(seq),
				ack:       p.header.seq,
				diff:      currentMicrosecond() - p.header.t,
				maxWindow: mtu,
				rto:       1000,
				state:     state_connected,

				outch:  make(chan *outgoingPacket, 10),
				sendch: make(chan *outgoingPacket, 10),
				recvch: make(chan *packet, 2),
				winch:  make(chan uint32),

				readch: make(chan []byte, 100),
				finch:  make(chan int, 1),

				keepalivech: make(chan time.Duration),

				recvbuf:   newPacketBuffer(window_size, int(p.header.seq)),
				sendbuf:   newPacketBuffer(window_size, seq),
				closefunc: func() error { return nil },
			}

			go c.loop()
			c.recvch <- &p

			l.conns.set(sid, &c)
			l.accept <- &c
		}
	}
}

func (l *UTPListener) Accept() (net.Conn, error) {
	return l.AcceptUTP()
}

func (l *UTPListener) AcceptUTP() (*UTPConn, error) {
	if l == nil || l.conn == nil {
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
		return conn, nil
	case err := <-l.err:
		l.lasterr = err
		return nil, err
	case <-timeout:
		return nil, &timeoutError{}
	}
}

func (l *UTPListener) Addr() net.Addr {
	return &UTPAddr{addr: l.conn.LocalAddr().(*net.UDPAddr)}
}

func (l *UTPListener) Close() error {
	if l == nil || l.conn == nil {
		return syscall.EINVAL
	}
	l.conns.close()
	return l.conn.Close()
}

func (l *UTPListener) SetDeadline(t time.Time) error {
	if l == nil || l.conn == nil {
		return syscall.EINVAL
	}
	l.deadline = t
	return nil
}

type connMap struct {
	m     map[uint16]*UTPConn
	mutex sync.RWMutex
}

func (m *connMap) set(id uint16, conn *UTPConn) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.m[id] = conn
}

func (m *connMap) delete(id uint16) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.m, id)
}

func (m *connMap) get(id uint16) *UTPConn {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.m[id]
}

func (m *connMap) contains(id uint16) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.m[id] != nil
}

func (m *connMap) close() {
	m.mutex.RLock()
	for _, c := range m.m {
		c.Close()
	}
	m.mutex.RUnlock()
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.m = nil
}
