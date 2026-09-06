// Package osc is a minimal Open Sound Control sender over UDP. It exists so
// ompool can drive external instruments (e.g. SuperCollider) in lockstep with
// what it renders, without pulling in a dependency for a few dozen bytes of
// framing.
package osc

import (
	"encoding/binary"
	"math"
	"net"
	"strings"
)

// Sender writes OSC messages to a single UDP destination. A nil *Sender is
// valid and silently drops every message, so callers can keep one code path
// whether or not OSC output is enabled.
type Sender struct {
	conn net.Conn
}

// New dials addr (host:port) for UDP. Nothing is sent until Send is called.
func New(addr string) (*Sender, error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, err
	}
	return &Sender{conn: conn}, nil
}

// Send emits one message. Every argument is encoded as an OSC float32 ("f"),
// which keeps the receiving side simple; block heights and satoshi counts fit
// float32 well enough for sonification.
func (s *Sender) Send(address string, args ...float32) {
	if s == nil || s.conn == nil {
		return
	}
	buf := make([]byte, 0, 64)
	buf = appendString(buf, address)
	buf = appendString(buf, ","+strings.Repeat("f", len(args)))
	for _, a := range args {
		buf = binary.BigEndian.AppendUint32(buf, math.Float32bits(a))
	}
	_, _ = s.conn.Write(buf)
}

// Close releases the socket. Safe on nil.
func (s *Sender) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

// appendString writes an OSC string: NUL-terminated, padded to a 4-byte boundary.
func appendString(buf []byte, s string) []byte {
	buf = append(buf, s...)
	buf = append(buf, 0)
	for len(buf)%4 != 0 {
		buf = append(buf, 0)
	}
	return buf
}
