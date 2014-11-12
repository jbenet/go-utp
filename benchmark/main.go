package main

import (
	"bytes"
	"crypto/md5"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"time"

	"github.com/davecheney/profile"
	"github.com/dustin/go-humanize"
	"github.com/h2so5/utp"
)

type RandReader struct{}

func (r RandReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = byte(rand.Int())
	}
	return len(p), nil
}

func main() {
	var l = flag.Int("c", 10485760, "Payload length (bytes)")
	var h = flag.Bool("h", false, "Human readable")
	flag.Parse()

	defer profile.Start(profile.CPUProfile).Stop()

	if *h {
		fmt.Printf("Payload: %s\n", humanize.IBytes(uint64(*l)))
	} else {
		fmt.Printf("Payload: %d\n", *l)
	}

	c2s := c2s(int64(*l))
	n, p := humanize.ComputeSI(c2s)
	if *h {
		fmt.Printf("C2S: %f%sbps\n", n, p)
	} else {
		fmt.Printf("C2S: %f\n", c2s)
	}

	s2c := s2c(int64(*l))
	n, p = humanize.ComputeSI(s2c)
	if *h {
		fmt.Printf("S2C: %f%sbps\n", n, p)
	} else {
		fmt.Printf("S2C: %f\n", s2c)
	}

	avg := (c2s + s2c) / 2.0
	n, p = humanize.ComputeSI(avg)

	if *h {
		fmt.Printf("AVG: %f%sbps\n", n, p)
	} else {
		fmt.Printf("AVG: %f\n", avg)
	}
}

func c2s(l int64) float64 {
	ln, err := utp.Listen("utp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}

	raddr, err := utp.ResolveUTPAddr("utp", ln.Addr().String())
	if err != nil {
		log.Fatal(err)
	}

	c, err := utp.DialUTPTimeout("utp", nil, raddr, 1000*time.Millisecond)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	if err != nil {
		log.Fatal(err)
	}

	s, err := ln.Accept()
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	ln.Close()

	rch := make(chan int)

	sendHash := md5.New()
	go func() {
		defer c.Close()
		io.Copy(io.MultiWriter(c, sendHash), io.LimitReader(RandReader{}, l))
	}()

	readHash := md5.New()
	go func() {
		io.Copy(readHash, s)
		rch <- 0
	}()

	start := time.Now()
	<-rch
	bps := float64(l*8) / (float64(time.Now().Sub(start)) / float64(time.Second))

	if !bytes.Equal(sendHash.Sum(nil), readHash.Sum(nil)) {
		log.Fatal("Broken payload")
	}

	return bps
}

func s2c(l int64) float64 {
	ln, err := utp.Listen("utp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}

	raddr, err := utp.ResolveUTPAddr("utp", ln.Addr().String())
	if err != nil {
		log.Fatal(err)
	}

	c, err := utp.DialUTPTimeout("utp", nil, raddr, 1000*time.Millisecond)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	if err != nil {
		log.Fatal(err)
	}

	s, err := ln.Accept()
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	ln.Close()

	rch := make(chan int)

	sendHash := md5.New()
	go func() {
		defer s.Close()
		io.Copy(io.MultiWriter(s, sendHash), io.LimitReader(RandReader{}, l))
	}()

	readHash := md5.New()
	go func() {
		io.Copy(readHash, c)
		rch <- 0
	}()

	start := time.Now()
	<-rch
	bps := float64(l*8) / (float64(time.Now().Sub(start)) / float64(time.Second))

	if !bytes.Equal(sendHash.Sum(nil), readHash.Sum(nil)) {
		log.Fatal("Broken payload")
	}

	return bps
}
