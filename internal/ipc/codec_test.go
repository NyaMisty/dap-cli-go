package ipc

import (
	"bytes"
	"testing"
)

func TestCodecStreamsMultipleEnvelopes(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	first := NewEnvelope(KindHello, "client.hello", "1", "", map[string]any{"token": "a"})
	second := NewEnvelope(KindRequest, "daemon.status", "2", "session", map[string]any{"token": "b"})
	if err := enc.Encode(first); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(second); err != nil {
		t.Fatal(err)
	}

	dec := NewDecoder(&buf)
	gotFirst, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	gotSecond, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if gotFirst.Kind != KindHello || gotFirst.Type != "client.hello" || gotFirst.Payload["token"] != "a" {
		t.Fatalf("unexpected first envelope: %#v", gotFirst)
	}
	if gotSecond.Kind != KindRequest || gotSecond.Type != "daemon.status" || gotSecond.SessionID != "session" || gotSecond.Payload["token"] != "b" {
		t.Fatalf("unexpected second envelope: %#v", gotSecond)
	}
}
