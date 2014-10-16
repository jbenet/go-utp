utp
===

uTP (Micro Transport Protocol) implementation

[![Build Status](https://travis-ci.org/h2so5/utp.svg)](https://travis-ci.org/h2so5/utp)
[![GoDoc](https://godoc.org/github.com/h2so5/utp?status.svg)](http://godoc.org/github.com/h2so5/utp)

**warning: This is a buggy alpha version.**

## Installation

```
go get github.com/h2so5/utp
```

## Example

Echo server
```go
package main

import "github.com/h2so5/utp"

func main() {
	addr, _ := utp.ResolveUTPAddr("utp", ":11000")
	ln, _ := utp.ListenUTP("utp", addr)
	defer ln.Close()

	conn, _ := ln.AcceptUTP()
	for {
		var buf [1024]byte
		l, err := conn.Read(buf[:])
		if err != nil {
			break
		}
		_, err = conn.Write(buf[:l])
		if err != nil {
			break
		}
	}
	conn.Close()
}

```
