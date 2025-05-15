package stringutil

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	input := "\x1b[31mred text\x1b[0m"
	expected := "red text"

	got := StripANSI(input)
	if got != expected {
		t.Errorf("stripANSI() = %q; want %q", got, expected)
	}
}

func TestPrintDotTable(t *testing.T) {
	rows := [][2]string{
		{"Name", "Value"},
		{"Foo", "Bar"},
		{"LongerName", "SomeText"},
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	PrintDotTable(rows)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	if err != nil {
		t.Fatalf("Failed to read stdout pipe: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Name") || !strings.Contains(output, "Value") {
		t.Errorf("printDotTable output missing expected content")
	}
}
