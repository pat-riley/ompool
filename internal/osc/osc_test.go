package osc

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestAppendStringPadsToFourBytes(t *testing.T) {
	cases := map[string]int{"": 4, "/a": 4, "/abc": 8, "/btc/tx": 8, ",ff": 4, ",fff": 8}
	for in, want := range cases {
		if got := len(appendString(nil, in)); got != want {
			t.Errorf("appendString(%q) length = %d, want %d", in, got, want)
		}
	}
}

func TestSendEncodesAddressTagsAndFloats(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sender, err := New(conn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()

	sender.Send("/btc/tx", 1.5, 2)

	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 128)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("/btc/tx\x00,ff\x00" +
		"\x3f\xc0\x00\x00" + // 1.5
		"\x40\x00\x00\x00") // 2.0
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("packet = % x, want % x", buf[:n], want)
	}
}

func TestNilSenderIsSafe(t *testing.T) {
	var s *Sender
	s.Send("/x", 1)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
