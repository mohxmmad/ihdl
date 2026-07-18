package internal

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestEvaluateShortCircuitGatesWithUndefinedInputs(t *testing.T) {
	andProject := &Project{
		Circuits: map[string]*Circuit{},
		Entry: &Circuit{
			Name:    "AndFeedback",
			Inputs:  []Port{{Name: "A", Kind: SignalBits, Width: 1}},
			Outputs: []Port{{Name: "OUT", Kind: SignalBits, Width: 1}},
			Wires:   []Port{{Name: "FB", Kind: SignalBits, Width: 1}},
			Signals: map[string]Port{"A": {Name: "A", Kind: SignalBits, Width: 1}, "OUT": {Name: "OUT", Kind: SignalBits, Width: 1}, "FB": {Name: "FB", Kind: SignalBits, Width: 1}},
			Ops:     []Operation{{Kind: "AND", Name: "G1", Inputs: []string{"A", "FB"}, Outputs: []string{"OUT"}}},
		},
	}

	outputs, err := Evaluate(andProject, andProject.Entry, map[string]Value{"A": {Kind: SignalBits, Bits: []bool{false}}})
	if err != nil {
		t.Fatalf("short-circuit and: %v", err)
	}
	if got := formatValue(outputs["OUT"]); got != "0" {
		t.Fatalf("expected OUT=0, got %s", got)
	}

	outputs, err = Evaluate(andProject, andProject.Entry, map[string]Value{"A": {Kind: SignalBits, Bits: []bool{true}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := formatValue(outputs["OUT"]); got != "err" {
		t.Fatalf("expected err when AND output is uncertain, got %s", got)
	}

	orProject := &Project{
		Circuits: map[string]*Circuit{},
		Entry: &Circuit{
			Name:    "OrFeedback",
			Inputs:  []Port{{Name: "A", Kind: SignalBits, Width: 1}},
			Outputs: []Port{{Name: "OUT", Kind: SignalBits, Width: 1}},
			Wires:   []Port{{Name: "FB", Kind: SignalBits, Width: 1}},
			Signals: map[string]Port{"A": {Name: "A", Kind: SignalBits, Width: 1}, "OUT": {Name: "OUT", Kind: SignalBits, Width: 1}, "FB": {Name: "FB", Kind: SignalBits, Width: 1}},
			Ops:     []Operation{{Kind: "OR", Name: "G1", Inputs: []string{"A", "FB"}, Outputs: []string{"OUT"}}},
		},
	}

	outputs, err = Evaluate(orProject, orProject.Entry, map[string]Value{"A": {Kind: SignalBits, Bits: []bool{true}}})
	if err != nil {
		t.Fatalf("short-circuit or: %v", err)
	}
	if got := formatValue(outputs["OUT"]); got != "1" {
		t.Fatalf("expected OUT=1, got %s", got)
	}

	outputs, err = Evaluate(orProject, orProject.Entry, map[string]Value{"A": {Kind: SignalBits, Bits: []bool{false}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := formatValue(outputs["OUT"]); got != "err" {
		t.Fatalf("expected err when OR output is uncertain, got %s", got)
	}
}

func TestGateWithExplicitErrInput(t *testing.T) {
	circ := &Circuit{
		Name:    "Top",
		Inputs:  []Port{{Name: "A", Kind: SignalBits, Width: 1}, {Name: "B", Kind: SignalBits, Width: 1}},
		Outputs: []Port{{Name: "O1", Kind: SignalBits, Width: 1}, {Name: "O2", Kind: SignalBits, Width: 1}},
		Signals: map[string]Port{"A": {Name: "A", Kind: SignalBits, Width: 1}, "B": {Name: "B", Kind: SignalBits, Width: 1}, "O1": {Name: "O1", Kind: SignalBits, Width: 1}, "O2": {Name: "O2", Kind: SignalBits, Width: 1}},
		Ops: []Operation{
			{Kind: "AND", Name: "G1", Inputs: []string{"A", "B"}, Outputs: []string{"O1"}},
			{Kind: "OR", Name: "G2", Inputs: []string{"A", "B"}, Outputs: []string{"O2"}},
		},
	}
	proj := &Project{Entry: circ, Circuits: map[string]*Circuit{"Top": circ}}

	outputs, err := Evaluate(proj, circ, map[string]Value{
		"A": {Kind: SignalErr},
		"B": {Kind: SignalBits, Bits: []bool{false}},
	})
	if err != nil {
		t.Fatalf("evaluate with err input: %v", err)
	}
	if got := formatValue(outputs["O1"]); got != "0" {
		t.Fatalf("AND(err,0): expected 0, got %s", got)
	}
	if got := formatValue(outputs["O2"]); got != "err" {
		t.Fatalf("OR(err,0): expected err, got %s", got)
	}

	outputs, err = Evaluate(proj, circ, map[string]Value{
		"A": {Kind: SignalErr},
		"B": {Kind: SignalBits, Bits: []bool{true}},
	})
	if err != nil {
		t.Fatalf("evaluate with err input: %v", err)
	}
	if got := formatValue(outputs["O1"]); got != "err" {
		t.Fatalf("AND(err,1): expected err, got %s", got)
	}
	if got := formatValue(outputs["O2"]); got != "1" {
		t.Fatalf("OR(err,1): expected 1, got %s", got)
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

func TestParseRejectsFlipFlops(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dff.ihdl")
	data := strings.Join([]string{
		"MODULE DFFExample",
		"INPUT D",
		"CLOCK CLK",
		"OUTPUT Q",
		"DFF FF1 D CLK Q",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}
	if _, err := ParseProject(path); err == nil || !strings.Contains(err.Error(), "unknown keyword DFF") {
		t.Fatalf("expected DFF parse error, got %v", err)
	}
}

func TestInteractiveSimulationRunsUntilStopAndUpdatesOutputs(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "and.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	input := bytes.NewBufferString("0\n0\nset A 1\nB 1\nstop\n")
	var output bytes.Buffer
	if err := simulateWithIO(project, SimulationOptions{}, input, &output); err != nil {
		t.Fatalf("simulate: %v", err)
	}

	text := output.String()
	if strings.Count(text, "OUT = 0") != 2 {
		t.Fatalf("expected two intermediate OUT = 0 states, got output %q", text)
	}
	if !strings.Contains(text, "OUT = 1") {
		t.Fatalf("expected live update to OUT = 1, got output %q", text)
	}
	if !strings.Contains(text, "Command (set <signal> <value> | clock <auto|manual|step> ... | show | stop):") {
		t.Fatalf("expected persistent command prompt, got output %q", text)
	}
}

func TestInteractiveManualClockStepping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clock_echo.ihdl")
	data := strings.Join([]string{
		"MODULE ClockEcho",
		"CLOCK CLK",
		"OUTPUT Q",
		"BUF B1 CLK Q",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}
	project, err := ParseProject(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	input := bytes.NewBufferString("0\nclock step CLK half\nclock step CLK full\nstop\n")
	var output bytes.Buffer
	if err := simulateWithIO(project, SimulationOptions{}, input, &output); err != nil {
		t.Fatalf("simulate: %v", err)
	}

	text := output.String()
	if strings.Count(text, "Q = 1") != 2 {
		t.Fatalf("expected two high states from half+full stepping, got output %q", text)
	}
	if strings.Count(text, "Q = 0") != 2 {
		t.Fatalf("expected initial and intermediate low states, got output %q", text)
	}
}

func TestParseClockCommands(t *testing.T) {
	declared := map[string]Port{"CLK": {Name: "CLK", Kind: SignalBits, Width: 1}}
	clocks := map[string]Port{"CLK": {Name: "CLK", Kind: SignalBits, Width: 1}}

	command, err := parseSimulationCommand("clock auto CLK 0.5", declared, clocks)
	if err != nil {
		t.Fatalf("parse auto: %v", err)
	}
	if command.kind != simulationCommandClockAuto || command.clockName != "CLK" || command.frequencyHz != 0.5 {
		t.Fatalf("unexpected auto command: %#v", command)
	}

	command, err = parseSimulationCommand("clock step CLK full", declared, clocks)
	if err != nil {
		t.Fatalf("parse step: %v", err)
	}
	if command.kind != simulationCommandClockStep || command.clockSteps != 2 {
		t.Fatalf("unexpected step command: %#v", command)
	}
}

func TestAdvanceAutoClocks(t *testing.T) {
	inputs := map[string]Value{"CLK": {Kind: SignalBits, Bits: []bool{false}}}
	now := time.Now()
	states := map[string]*clockState{
		"CLK": {mode: clockModeAuto, frequency: 0.5, nextToggle: now.Add(-250 * time.Millisecond)},
	}

	if !advanceAutoClocks(inputs, states, now.Add(2250*time.Millisecond)) {
		t.Fatalf("expected auto clock advance")
	}
	if got := formatValue(inputs["CLK"]); got != "1" {
		t.Fatalf("expected CLK to toggle three times to 1, got %s", got)
	}
	if !states["CLK"].nextToggle.After(now.Add(2250 * time.Millisecond)) {
		t.Fatalf("expected next toggle to move into the future")
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

func TestDisplayRendersRGBAndBWSignals(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "display_rgb.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	_, err = Evaluate(project, project.Entry, map[string]Value{
		"P0": {Kind: SignalRGB, Channels: []uint8{10, 20, 30}},
		"P1": {Kind: SignalBW, Channels: []uint8{200}},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	frame, ok := project.Frames["DisplayRGB.SCREEN"]
	if !ok {
		t.Fatalf("expected display frame to be rendered")
	}
	if frame.Width != 2 || frame.Height != 1 {
		t.Fatalf("unexpected frame size: %dx%d", frame.Width, frame.Height)
	}
	if got := frame.Pixels[:6]; got[0] != 10 || got[1] != 20 || got[2] != 30 || got[3] != 200 || got[4] != 200 || got[5] != 200 {
		t.Fatalf("unexpected frame pixels: %v", got)
	}
}

func TestDisplayDefaultsUndrivenPixelsToBlack(t *testing.T) {
	project := &Project{
		Circuits: map[string]*Circuit{},
		Entry: &Circuit{
			Name:    "Top",
			Inputs:  []Port{{Name: "PX", Kind: SignalRGB, Width: 0}},
			Outputs: []Port{{Name: "PX_OUT", Kind: SignalRGB, Width: 0}},
			Signals: map[string]Port{"PX": {Name: "PX", Kind: SignalRGB}, "PX_OUT": {Name: "PX_OUT", Kind: SignalRGB}},
			Displays: []Display{{
				Name:   "SCREEN",
				Width:  2,
				Height: 1,
				Pixels: []DisplayPixel{{X: 0, Y: 0, Signal: "PX_OUT"}, {X: 1, Y: 0, Signal: "MISSING"}},
			}},
			Ops: []Operation{{Kind: "BUF", Name: "B1", Inputs: []string{"PX"}, Outputs: []string{"PX_OUT"}}},
		},
	}

	_, err := Evaluate(project, project.Entry, map[string]Value{"PX": {Kind: SignalRGB, Channels: []uint8{1, 2, 3}}})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	frame := project.Frames["Top.SCREEN"]
	if got := frame.Pixels[3:6]; got[0] != 0 || got[1] != 0 || got[2] != 0 {
		t.Fatalf("expected undriven pixel to stay black, got %v", got)
	}
}

func TestWriteDisplayFilesExportsIPPF(t *testing.T) {
	project, err := ParseProject(filepath.Join("..", "examples", "display_rgb.ihdl"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Evaluate(project, project.Entry, map[string]Value{
		"P0": {Kind: SignalRGB, Channels: []uint8{255, 0, 0}},
		"P1": {Kind: SignalBW, Channels: []uint8{0}},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	path := filepath.Join(t.TempDir(), "screen.ippf")
	if err := WriteDisplayFiles(project, path); err != nil {
		t.Fatalf("write display: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read display: %v", err)
	}
	pngHeader := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if len(data) < len(pngHeader) || string(data[:len(pngHeader)]) != string(pngHeader) {
		t.Fatalf("expected ippf export to contain png data")
	}
}
