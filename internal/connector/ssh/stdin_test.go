package ssh

import (
	"bytes"
	"testing"
)

type fakeWriteCloser struct {
	bytes.Buffer
	closed bool
}

func (f *fakeWriteCloser) Close() error {
	f.closed = true
	return nil
}

func TestClientSendEOFClosesPipeMode(t *testing.T) {
	pipe := &fakeWriteCloser{}
	client := &Client{}
	client.setStdinPipe(pipe, false)

	if err := client.SendEOF(); err != nil {
		t.Fatalf("SendEOF() error = %v", err)
	}
	if !pipe.closed {
		t.Fatal("expected pipe mode SendEOF to close stdin")
	}
	if client.stdinPipe != nil {
		t.Fatal("expected stdin pipe to be cleared after pipe EOF")
	}
}

func TestClientSendEOFWritesCtrlDForPTY(t *testing.T) {
	pipe := &fakeWriteCloser{}
	client := &Client{}
	client.setStdinPipe(pipe, true)

	if err := client.SendEOF(); err != nil {
		t.Fatalf("SendEOF() error = %v", err)
	}
	if pipe.closed {
		t.Fatal("expected PTY mode SendEOF not to close stdin")
	}
	if got := pipe.Bytes(); len(got) != 1 || got[0] != 0x04 {
		t.Fatalf("expected PTY mode SendEOF to write 0x04, got %v", got)
	}
}
