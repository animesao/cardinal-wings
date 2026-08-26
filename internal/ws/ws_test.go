package ws

import (
	"bufio"
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := writeFrame(w, 0x1, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	r := bufio.NewReader(&buf)
	fin, opcode, payload, err := readFrame(r)
	if err != nil {
		t.Fatal(err)
	}
	if !fin {
		t.Error("expected FIN")
	}
	if opcode != 0x1 {
		t.Errorf("opcode = %d, want 1", opcode)
	}
	if string(payload) != "hello" {
		t.Errorf("payload = %q, want hello", payload)
	}
}

func TestMaskedFrame(t *testing.T) {
	// Client frames are masked; simulate one: FIN+text, masked, len 3, mask 01020304.
	payload := []byte{'a', 'b', 'c'}
	mask := []byte{0x01, 0x02, 0x03, 0x04}
	frame := []byte{0x81, 0x83, mask[0], mask[1], mask[2], mask[3]}
	for i, p := range payload {
		frame = append(frame, p^mask[i%4])
	}
	r := bufio.NewReader(bytes.NewReader(frame))
	_, opcode, out, err := readFrame(r)
	if err != nil {
		t.Fatal(err)
	}
	if opcode != 0x1 {
		t.Errorf("opcode = %d, want 1", opcode)
	}
	if string(out) != "abc" {
		t.Errorf("unmasked = %q, want abc", out)
	}
}

func TestLargeFrameHeader(t *testing.T) {
	// 16-bit length path.
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	data := bytes.Repeat([]byte("x"), 300)
	if err := writeFrame(w, 0x1, data); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(&buf)
	_, _, out, err := readFrame(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 300 {
		t.Errorf("len = %d, want 300", len(out))
	}
}
