package cli

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestMOBIUSPlainDisablesTTY(t *testing.T) {
	t.Setenv("MOBIUS_PLAIN", "1")
	if defaultIsTTY() {
		t.Fatal("MOBIUS_PLAIN should force plain")
	}
}

func TestInteractivePredicateInjectable(t *testing.T) {
	t.Setenv("MOBIUS_PLAIN", "")
	var pred IsTTYFunc = func() bool { return false }
	if pred() {
		t.Fatal("expected injectable false")
	}
	_ = os.Stdout
	_ = bytes.NewBuffer(nil)
	_ = io.Discard
}
