package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluateAnd(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "and.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	outputs, err := Evaluate(project, project.Entry, map[string]Value{
		"A": {Kind: SignalBits, Bits: []bool{true}},
		"B": {Kind: SignalBits, Bits: []bool{false}},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if outputs["OUT"].Bits[0] {
		t.Fatalf("expected OUT=0, got 1")
	}

	outputs, err = Evaluate(project, project.Entry, map[string]Value{
		"A": {Kind: SignalBits, Bits: []bool{true}},
		"B": {Kind: SignalBits, Bits: []bool{true}},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !outputs["OUT"].Bits[0] {
		t.Fatalf("expected OUT=1, got 0")
	}
}

func TestEvaluateSplitAndConstants(t *testing.T) {
	project := &Project{
		Circuits: map[string]*Circuit{},
		Entry: &Circuit{
			Name:    "Top",
			Inputs:  []Port{{Name: "BUS", Kind: SignalBits, Width: 2}},
			Outputs: []Port{{Name: "LEFT", Kind: SignalBits, Width: 1}, {Name: "RIGHT", Kind: SignalBits, Width: 1}, {Name: "ONE", Kind: SignalBits, Width: 1}},
			Signals: map[string]Port{"BUS": {Name: "BUS", Kind: SignalBits, Width: 2}, "LEFT": {Name: "LEFT", Kind: SignalBits, Width: 1}, "RIGHT": {Name: "RIGHT", Kind: SignalBits, Width: 1}, "ONE": {Name: "ONE", Kind: SignalBits, Width: 1}},
			Ops:     []Operation{{Kind: "SPLIT", Inputs: []string{"BUS"}, Outputs: []string{"LEFT", "RIGHT"}}, {Kind: "HIGH", Outputs: []string{"ONE"}}},
		},
	}

	outputs, err := Evaluate(project, project.Entry, map[string]Value{"BUS": {Kind: SignalBits, Bits: []bool{true, false}}})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !outputs["LEFT"].Bits[0] || outputs["RIGHT"].Bits[0] || !outputs["ONE"].Bits[0] {
		t.Fatalf("unexpected outputs: %#v", outputs)
	}
	project.Circuits["top"] = project.Entry
}

func TestEvaluateJoin(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "join.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	outputs, err := Evaluate(project, project.Entry, map[string]Value{
		"B0": {Kind: SignalBits, Bits: []bool{true}},
		"B1": {Kind: SignalBits, Bits: []bool{false}},
		"B2": {Kind: SignalBits, Bits: []bool{true}},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got := formatValue(outputs["BUS"]); got != "101" {
		t.Fatalf("expected BUS=101, got %s", got)
	}
}

func TestEvaluateImportedModule(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "invert_and.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	outputs, err := Evaluate(project, project.Entry, map[string]Value{"IN": {Kind: SignalBits, Bits: []bool{false}}})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !outputs["OUT"].Bits[0] {
		t.Fatalf("expected OUT=1 for imported inverter")
	}

	outputs, err = Evaluate(project, project.Entry, map[string]Value{"IN": {Kind: SignalBits, Bits: []bool{true}}})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if outputs["OUT"].Bits[0] {
		t.Fatalf("expected OUT=0 for imported inverter")
	}
}

func TestImportedModuleRequiresExplicitSignals(t *testing.T) {
	if _, err := ParseProject(filepath.Join("..", "examples", "bad_use.ihdl")); err == nil {
		t.Fatalf("expected parse error for missing instance signals")
	}
}

func TestSimulateFromFiles(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "and.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "case.iinp")
	outputPath := filepath.Join(tempDir, "case.iout")

	if err := os.WriteFile(inputPath, []byte("A 1\nB 1\n"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	if err := SimulateFromFiles(project, inputPath, outputPath); err != nil {
		t.Fatalf("simulate from files: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "OUT 1\n" {
		t.Fatalf("unexpected output file: %q", string(data))
	}
}

func TestEvaluateNamedBusOutput(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "bus_not.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	outputs, err := Evaluate(project, project.Entry, map[string]Value{"DATA": {Kind: SignalBits, Bits: []bool{true, false, true}}})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got := formatValue(outputs["FINAL"]); got != "010" {
		t.Fatalf("expected FINAL=010, got %s", got)
	}
}

func TestClockIsAcceptedInEvaluationAndFiles(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "clocked_and.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	outputs, err := Evaluate(project, project.Entry, map[string]Value{
		"A":   {Kind: SignalBits, Bits: []bool{true}},
		"B":   {Kind: SignalBits, Bits: []bool{true}},
		"CLK": {Kind: SignalBits, Bits: []bool{false}},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !outputs["RESULT"].Bits[0] {
		t.Fatalf("expected RESULT=1")
	}

	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "clocked.iinp")
	outputPath := filepath.Join(tempDir, "clocked.iout")
	if err := os.WriteFile(inputPath, []byte("A 1\nB 1\nCLK 1\n"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if err := SimulateFromFiles(project, inputPath, outputPath); err != nil {
		t.Fatalf("simulate from files: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "RESULT 1\n" {
		t.Fatalf("unexpected output file: %q", string(data))
	}
}

func TestRGBAndBWPixels(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "rgb_passthrough.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	outputs, err := Evaluate(project, project.Entry, map[string]Value{
		"PX":    {Kind: SignalRGB, Channels: []uint8{255, 64, 0}},
		"LEVEL": {Kind: SignalBW, Channels: []uint8{200}},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got := formatValue(outputs["PX_OUT"]); got != "255,64,0" {
		t.Fatalf("unexpected rgb output %s", got)
	}
	if got := formatValue(outputs["BW_OUT"]); got != "200" {
		t.Fatalf("unexpected bw output %s", got)
	}
}

func TestRGBModuleGridComposition(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "rgb_row.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	outputs, err := Evaluate(project, project.Entry, map[string]Value{
		"P1": {Kind: SignalRGB, Channels: []uint8{1, 2, 3}},
		"P2": {Kind: SignalRGB, Channels: []uint8{4, 5, 6}},
		"P3": {Kind: SignalRGB, Channels: []uint8{7, 8, 9}},
		"L1": {Kind: SignalBW, Channels: []uint8{10}},
		"L2": {Kind: SignalBW, Channels: []uint8{20}},
		"L3": {Kind: SignalBW, Channels: []uint8{30}},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if formatValue(outputs["O2"]) != "4,5,6" {
		t.Fatalf("unexpected row output %s", formatValue(outputs["O2"]))
	}
}

func TestPixelFileIO(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "rgb_passthrough.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "pixel.iinp")
	outputPath := filepath.Join(tempDir, "pixel.iout")
	if err := os.WriteFile(inputPath, []byte("PX 12,34,56\nLEVEL 128\n"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if err := SimulateFromFiles(project, inputPath, outputPath); err != nil {
		t.Fatalf("simulate from files: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "PX_OUT 12,34,56\nBW_OUT 128\n" {
		t.Fatalf("unexpected output file: %q", string(data))
	}
}

func TestBWAcceptsBinaryByteInput(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "rgb_passthrough.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "pixel_binary.iinp")
	outputPath := filepath.Join(tempDir, "pixel_binary.iout")
	if err := os.WriteFile(inputPath, []byte("PX 1,2,3\nLEVEL 10101010\n"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if err := SimulateFromFiles(project, inputPath, outputPath); err != nil {
		t.Fatalf("simulate from files: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "PX_OUT 1,2,3\nBW_OUT 170\n" {
		t.Fatalf("unexpected output file: %q", string(data))
	}
}

func TestRGBAcceptsBinaryChannelInput(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "rgb_passthrough.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "rgb_binary.iinp")
	outputPath := filepath.Join(tempDir, "rgb_binary.iout")
	if err := os.WriteFile(inputPath, []byte("PX 11111111,00000000,10101010\nLEVEL 0\n"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if err := SimulateFromFiles(project, inputPath, outputPath); err != nil {
		t.Fatalf("simulate from files: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "PX_OUT 255,0,170\nBW_OUT 0\n" {
		t.Fatalf("unexpected output file: %q", string(data))
	}
}
