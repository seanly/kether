package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestTwoNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var buf bytes.Buffer
	if err := run(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "public ") || !strings.Contains(out, "grant ") || !strings.Contains(out, "ok\n") {
		t.Fatalf("output %q", out)
	}
	if strings.Contains(out, "preimage") {
		t.Fatal("output leaked a preimage")
	}
}
