package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"reflect"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/h2so5/utp"
)

func main() {
	var l = flag.Int("c", 10485760, "Payload length (bytes)")
	var h = flag.Bool("h", false, "Human readable")
	flag.Parse()

	payload := make([]byte, *l)
	for i := range payload {
		payload[i] = byte(rand.Int())
	}

	if *h {
		fmt.Printf("Payload: %s\n", humanize.IBytes(uint64(len(payload))))
	} else {
		fmt.Printf("Payload: %d\n", len(payload))
	}

	c2s := c2s(payload)
	n, p := humanize.ComputeSI(c2s)
	if *h {
		fmt.Printf("C2S: %f%sbps\n", n, p)
	} else {
		fmt.Printf("C2S: %f\n", c2s)
	}

	s2c := s2c(payload)
	n, p = humanize.ComputeSI(s2c)
	if *h {
		fmt.Printf("S2C: %f%sbps\n", n, p)
	} else {
		fmt.Printf("S2C: %f\n", c2s)
	}

	avg := (c2s + s2c) / 2.0
	n, p = humanize.ComputeSI(avg)

	if *h {
		fmt.Printf("AVG: %f%sbps\n", n, p)
	} else {
		fmt.Printf("AVG: %f\n", avg)
	}
}

func c2s(payload []byte) float64 {
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

	rch := make(chan []byte)

	go func() {
		defer c.Close()
		c.Write(payload[:])
	}()

	go func() {
		b, _ := ioutil.ReadAll(s)
		rch <- b
	}()

	start := time.Now()
	r := <-rch
	bps := float64(len(payload)*8) / (float64(time.Now().Sub(start)) / float64(time.Second))

	if !reflect.DeepEqual(payload, r) {
		log.Fatal("Broken payload")
	}

	return bps
}

func s2c(payload []byte) float64 {
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

	rch := make(chan []byte)

	go func() {
		defer s.Close()
		s.Write(payload[:])
	}()

	go func() {
		b, _ := ioutil.ReadAll(c)
		rch <- b
	}()

	start := time.Now()
	r := <-rch
	bps := float64(len(payload)*8) / (float64(time.Now().Sub(start)) / float64(time.Second))

	if !reflect.DeepEqual(payload, r) {
		log.Fatal("Broken payload")
	}

	return bps
}
