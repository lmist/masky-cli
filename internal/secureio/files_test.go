package secureio

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "result")
	write := func(s string) func(io.Writer) error {
		return func(w io.Writer) error { _, err := io.WriteString(w, s); return err }
	}
	if err := Write(path, false, write("original")); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, false, write("replacement")); err == nil {
		t.Fatal("overwrote existing file")
	}
	err := Write(path, true, func(w io.Writer) error { _, _ = io.WriteString(w, "partial"); return errors.New("failed") })
	if err == nil {
		t.Fatal("lost write error")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "original" {
		t.Fatal("corrupted existing file")
	}
	if err := Write(path, true, write("new")); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatal("temporary files left behind")
	}
}
func TestBoundedRead(t *testing.T) {
	if _, err := Read(strings.NewReader("1234"), 3); err == nil {
		t.Fatal("size limit ignored")
	}
	if b, err := Read(strings.NewReader("123"), 3); err != nil || string(b) != "123" {
		t.Fatal("boundary failed")
	}
}
