package utp

import (
	"time"
)

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
