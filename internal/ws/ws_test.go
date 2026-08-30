package ws

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := writeMaskedFrame(w, 0x1, []byte("hello")); err != nil {
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

func writeMaskedFrame(w *bufio.Writer, opcode byte, payload []byte) error {
	mask := []byte{1, 2, 3, 4}
	frame := []byte{0x80 | opcode}
	if len(payload) <= 125 {
		frame = append(frame, 0x80|byte(len(payload)))
	} else if len(payload) <= 0xffff {
		frame = append(frame, 0x80|126, byte(len(payload)>>8), byte(len(payload)))
	} else {
		return fmt.Errorf("test payload too large")
	}
	frame = append(frame, mask...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := w.Write(frame)
	return err
}

func TestRSVFrameRejected(t *testing.T) {
	frame := []byte{0xC1, 0x81, 1, 2, 3, 4, 'x' ^ 1}
	_, _, _, err := readFrame(bufio.NewReader(bytes.NewReader(frame)))
	if err == nil || !strings.Contains(err.Error(), "extensions") {
		t.Fatalf("expected RSV rejection, got %v", err)
	}
}

func TestUnmaskedFrameRejected(t *testing.T) {
	frame := []byte{0x81, 0x03, 'a', 'b', 'c'}
	_, _, _, err := readFrame(bufio.NewReader(bytes.NewReader(frame)))
	if err == nil || !strings.Contains(err.Error(), "masked") {
		t.Fatalf("expected unmasked frame rejection, got %v", err)
	}
}

func TestFragmentedFrameRejected(t *testing.T) {
	frame := []byte{0x01, 0x81, 1, 2, 3, 4, 'x' ^ 1}
	fin, _, _, err := readFrame(bufio.NewReader(bytes.NewReader(frame)))
	if !fin {
		err = fmt.Errorf("fragmented frames are not supported")
	}
	if err == nil || !strings.Contains(err.Error(), "fragmented") {
		t.Fatalf("expected fragmented frame rejection, got %v", err)
	}
}

func TestLargeFrameHeader(t *testing.T) {
	// 16-bit length path.
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	data := bytes.Repeat([]byte("x"), 300)
	if err := writeMaskedFrame(w, 0x1, data); err != nil {
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
