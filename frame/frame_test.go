package frame_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/seanly/kether/frame"
)

func TestRoundTrip(t *testing.T) {
	env := &frame.Envelope{CorrID: 7, Kind: frame.KindInvoke, Invoke: frame.Invoke{
		Type: "echo", Body: []byte("hi"), Nonce: []byte{1, 2}, SentAt: 10,
	}}
	var buf bytes.Buffer
	if err := frame.Write(&buf, env); err != nil {
		t.Fatal(err)
	}
	got, err := frame.Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.CorrID != 7 || got.Kind != frame.KindInvoke || got.Invoke.Type != "echo" || string(got.Invoke.Body) != "hi" {
		t.Fatalf("%+v", got)
	}
}

func TestTooLarge(t *testing.T) {
	var buf bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(frame.MaxFrame+1))
	buf.Write(hdr[:])
	if _, err := frame.Read(&buf); err == nil {
		t.Fatal("expected too large")
	}
}
