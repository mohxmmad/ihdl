package internal

import (
	"os"
	"testing"
)

// icpu is a sibling project; these tests exercise the real RAM32K hierarchy
// that previously consumed tens of gigabytes of WireState. They are skipped
// automatically when the icpu checkout is not present.

const (
	icpuRAM32KPath  = "/media/mohammad/4ced7ac4-815e-4531-89de-145a9460e9461/Documents/icpu/cpu/memory/ram32k.ihdl"
	icpuIRAM32KPath = "/media/mohammad/4ced7ac4-815e-4531-89de-145a9460e9461/Documents/icpu/cpu/memory/iram32k.ihdl"
)

func haveIcpu(t *testing.T) bool {
	t.Helper()
	if _, err := os.Stat(icpuRAM32KPath); err != nil {
		t.Skipf("icpu checkout not present: %v", err)
		return false
	}
	return true
}

func bits16(s string) Value {
	v, err := parseBits(s, 16)
	if err != nil {
		panic(err)
	}
	return Value{Kind: SignalBits, Bits: v}
}

func bitsN(s string) Value {
	v, err := parseBits(s, len(s))
	if err != nil {
		panic(err)
	}
	return Value{Kind: SignalBits, Bits: v}
}

func bit(v bool) Value {
	return Value{Kind: SignalBits, Bits: []bool{v}}
}

// evalRAM32K drives the RAM32K entry once and returns its OUT as a binary
// string. in/load/reset/addr/clk select the driving values.
func evalRAM32K(p *Project, in string, load, reset bool, addr string, clk bool) (string, error) {
	outputs, err := Evaluate(p, p.Entry, map[string]Value{
		"IN":    bits16(in),
		"LOAD":  bit(load),
		"RESET": bit(reset),
		"ADDR":  bitsN(addr),
		"CLK":   bit(clk),
	})
	if err != nil {
		return "", err
	}
	return formatValue(outputs["OUT"]), nil
}

// TestIcpuRAM32kWriteRead verifies a write to one word is read back from that
// word and does not alias the adjacent word. This is the compact-state
// regression test: before the unique per-instance cache ids, stateless
// multiplexers shared a cache slot and every read returned the same value.
func TestIcpuRAM32kWriteRead(t *testing.T) {
	if !haveIcpu(t) {
		return
	}
	project, err := ParseProject(icpuRAM32KPath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	word0 := "000000000000000" // bank 0, word 0
	word1 := "000000000000001" // bank 0, word 1
	data := "0000000000001010" // 10

	// Reset everything to zero.
	out, err := evalRAM32K(project, "0000000000000000", false, true, word0, false)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if out != "0000000000000000" {
		t.Fatalf("after reset expected zeros, got %s", out)
	}

	// Write data to word0: set up on CLK=0, latch on CLK=1.
	if _, err := evalRAM32K(project, data, true, false, word0, false); err != nil {
		t.Fatalf("write setup: %v", err)
	}
	if _, err := evalRAM32K(project, data, true, false, word0, true); err != nil {
		t.Fatalf("write latch: %v", err)
	}
	if _, err := evalRAM32K(project, data, true, false, word0, false); err != nil {
		t.Fatalf("write hold: %v", err)
	}

	// Read back word0 (LOAD=0 so nothing writes).
	out, err = evalRAM32K(project, "0000000000000000", false, false, word0, false)
	if err != nil {
		t.Fatalf("read word0: %v", err)
	}
	if out != data {
		t.Fatalf("read back word0: expected %s, got %s", data, out)
	}

	// A different word should still be zero.
	out, err = evalRAM32K(project, "0000000000000000", false, false, word1, false)
	if err != nil {
		t.Fatalf("read word1: %v", err)
	}
	if out != "0000000000000000" {
		t.Fatalf("word1 should be untouched, got %s", out)
	}
}
