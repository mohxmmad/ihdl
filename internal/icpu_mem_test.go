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

func TestSkippedUseRestoresStateWithoutCache(t *testing.T) {
	child := &Circuit{
		Name:    "Child",
		Inputs:  []Port{{Name: "IN", Kind: SignalBits, Width: 1}, {Name: "CTRL", Kind: SignalBits, Width: 1}},
		Outputs: []Port{{Name: "OUT", Kind: SignalBits, Width: 1}},
		Signals: map[string]Port{
			"IN":   {Name: "IN", Kind: SignalBits, Width: 1},
			"CTRL": {Name: "CTRL", Kind: SignalBits, Width: 1},
			"FB":   {Name: "FB", Kind: SignalBits, Width: 1},
			"OUT":  {Name: "OUT", Kind: SignalBits, Width: 1},
		},
		Ops: []Operation{
			{Kind: "BUF", Name: "B1", Inputs: []string{"IN"}, Outputs: []string{"FB"}},
			{Kind: "BUF", Name: "B2", Inputs: []string{"FB"}, Outputs: []string{"OUT"}},
			{Kind: "BUF", Name: "B3", Inputs: []string{"OUT"}, Outputs: []string{"FB"}},
		},
	}
	parent := &Circuit{
		Name:    "Parent",
		Inputs:  []Port{{Name: "A", Kind: SignalBits, Width: 1}},
		Outputs: []Port{{Name: "OUT", Kind: SignalBits, Width: 1}},
		Signals: map[string]Port{
			"A":   {Name: "A", Kind: SignalBits, Width: 1},
			"X":   {Name: "X", Kind: SignalBits, Width: 1},
			"OUT": {Name: "OUT", Kind: SignalBits, Width: 1},
		},
		Ops: []Operation{
			{Kind: "IGNORE", Name: "G1", Inputs: []string{"A"}, Outputs: []string{"X"}},
			{Kind: "USE", Name: "C1", Module: "Child", Signals: []string{"A", "X", "OUT"}},
		},
	}
	proj := &Project{Entry: parent, Circuits: map[string]*Circuit{"Parent": parent, "Child": child}}

	outputs, err := Evaluate(proj, parent, map[string]Value{"A": {Kind: SignalBits, Bits: []bool{true}}})
	if err != nil {
		t.Fatalf("evaluate first: %v", err)
	}
	if got := formatValue(outputs["OUT"]); got != "1" {
		t.Fatalf("expected first OUT=1, got %s", got)
	}

	proj.comp.outCache = make(map[int][]Value)

	outputs, err = Evaluate(proj, parent, map[string]Value{"A": {Kind: SignalBits, Bits: []bool{false}}})
	if err != nil {
		t.Fatalf("evaluate second: %v", err)
	}
	if got := formatValue(outputs["OUT"]); got != "1" {
		t.Fatalf("expected skipped child to restore stateful OUT=1 without cache, got %s", got)
	}
}

func TestVRAM8SkippableFlags(t *testing.T) {
	path := "/media/mohammad/4ced7ac4-815e-4531-89de-145a9460e9461/Documents/icpu/cpu/memory/vram8.ihdl"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("icpu checkout not present: %v", err)
	}
	project, err := ParseProject(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := ensureCompiled(project, project.Entry); err != nil {
		t.Fatalf("compile: %v", err)
	}
	cc := project.comp.modules[project.Entry.Name]
	if cc == nil {
		t.Fatalf("missing compiled entry")
	}
	if !cc.ctxCanIgnore["A0"] || !cc.ctxCanIgnore["L0"] {
		t.Fatalf("expected reset-decoder path to make A0 and L0 skippable")
	}
	for i, op := range project.Entry.Ops {
		if op.Kind == "USE" && op.Module == "REGISTER" {
			if !cc.skippable[i] {
				t.Fatalf("REGISTER use %s at op %d is not skippable", op.Name, i)
			}
		}
	}
}
