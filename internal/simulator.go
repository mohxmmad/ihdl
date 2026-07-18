package internal

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func Simulate(project *Project) error {
	return SimulateWithOptions(project, SimulationOptions{})
}

func SimulateWithOptions(project *Project, options SimulationOptions) error {
	return simulateWithIO(project, options, os.Stdin, os.Stdout)
}

func simulateWithIO(project *Project, options SimulationOptions, input io.Reader, output io.Writer) error {
	reader := bufio.NewReader(input)
	var viewer *displayViewer
	ports := allSourcePorts(project.Entry)
	inputs := make(map[string]Value, len(ports))
	declared := make(map[string]Port, len(ports))
	clocks := make(map[string]Port, len(project.Entry.Clocks))

	for _, port := range ports {
		declared[port.Name] = port
		fmt.Fprint(output, inputPrompt(port))
		text, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			value, err := parseValue(trimmed, port)
			if err != nil {
				return err
			}
			inputs[port.Name] = value
		}
	}
	for _, port := range project.Entry.Clocks {
		clocks[port.Name] = port
	}
	clockStates := initializeClockStates(project.Entry.Clocks)

	var err error
	viewer, err = evaluateAndRenderStep(project, options, inputs, viewer, output)
	if err != nil {
		return err
	}
	printSimulationPrompt(output)

	lines := make(chan simulationLine)
	go readSimulationLines(reader, lines)

	for {
		timer, timerCh := nextAutoClockTimer(clockStates, time.Now())
		select {
		case line, ok := <-lines:
			stopClockTimer(timer)
			if !ok || (line.err == io.EOF && strings.TrimSpace(line.text) == "") {
				return nil
			}
			if line.err != nil {
				return line.err
			}

			command, err := parseSimulationCommand(line.text, declared, clocks)
			if err != nil {
				fmt.Fprintf(output, "%v\n", err)
				printSimulationPrompt(output)
				continue
			}

			rerender := true
			switch command.kind {
			case simulationCommandStop:
				return nil
			case simulationCommandSet:
				inputs[command.port.Name] = command.value
			case simulationCommandClockAuto:
				if err := setClockAuto(clockStates, command.clockName, command.frequencyHz, time.Now()); err != nil {
					fmt.Fprintf(output, "%v\n", err)
					printSimulationPrompt(output)
					continue
				}
			case simulationCommandClockManual:
				if err := setClockManual(clockStates, command.clockName); err != nil {
					fmt.Fprintf(output, "%v\n", err)
					printSimulationPrompt(output)
					continue
				}
			case simulationCommandClockStep:
				viewer, err = stepManualClock(project, options, inputs, viewer, output, clockStates, command.clockName, command.clockSteps)
				if err != nil {
					fmt.Fprintf(output, "%v\n", err)
					printSimulationPrompt(output)
					continue
				}
				rerender = false
			}

			if rerender {
				viewer, err = evaluateAndRenderStep(project, options, inputs, viewer, output)
				if err != nil {
					return err
				}
			}
			printSimulationPrompt(output)

		case now := <-timerCh:
			if advanceAutoClocks(inputs, clockStates, now) {
				viewer, err = evaluateAndRenderStep(project, options, inputs, viewer, output)
				if err != nil {
					return err
				}
				printSimulationPrompt(output)
			}
		}
	}
}

func SimulateFromFiles(project *Project, inputPath, outputPath string) error {
	return SimulateFromFilesWithOptions(project, inputPath, outputPath, SimulationOptions{})
}

func SimulateFromFilesWithOptions(project *Project, inputPath, outputPath string, options SimulationOptions) error {
	inputs, err := ReadInputFile(project.Entry, inputPath)
	if err != nil {
		return err
	}

	outputs, err := Evaluate(project, project.Entry, inputs)
	if err != nil {
		return err
	}
	if options.DisplayExportPath != "" {
		if err := WriteDisplayFiles(project, options.DisplayExportPath); err != nil {
			return err
		}
	}

	return WriteOutputFile(project.Entry, outputPath, outputs)
}

func Evaluate(project *Project, circuit *Circuit, inputs map[string]Value) (map[string]Value, error) {
	return evaluate(project, circuit, inputs, circuit.Name)
}

func evaluate(project *Project, circuit *Circuit, inputs map[string]Value, scope string) (map[string]Value, error) {
	env := make(map[string]Value)
	for _, in := range allSourcePorts(circuit) {
		value, ok := inputs[in.Name]
		if ok {
			if err := ensurePortValue(in, value, circuit.Name); err != nil {
				return nil, err
			}
			env[in.Name] = cloneValue(value)
		}
	}

	modulesByName := make(map[string]*Circuit)
	for _, imported := range project.Circuits {
		modulesByName[imported.Name] = imported
	}

	pending := append([]Operation(nil), circuit.Ops...)
	for len(pending) > 0 {
		progress := false
		next := make([]Operation, 0, len(pending))

		for _, op := range pending {
			applied, err := applyOperation(op, env, circuit, modulesByName, project, scope)
			if err != nil {
				return nil, err
			}
			if applied {
				progress = true
				continue
			}
			next = append(next, op)
		}

		if !progress {
			for _, op := range next {
				for _, out := range op.Outputs {
					env[out] = errValue(op)
				}
			}
			break
		}
		pending = next
	}

	outputs := make(map[string]Value, len(circuit.Outputs))
	if err := renderDisplays(project, circuit, env, scope); err != nil {
		return nil, err
	}
	for _, out := range circuit.Outputs {
		value, ok := env[out.Name]
		if !ok {
			value = errValue()
		}
		outputs[out.Name] = cloneValue(value)
	}

	return outputs, nil
}

func errValue(ops ...Operation) Value {
	return Value{Kind: SignalErr}
}

func applyOperation(op Operation, env map[string]Value, circuit *Circuit, modules map[string]*Circuit, project *Project, scope string) (bool, error) {
	switch op.Kind {
	case "HIGH", "LOW":
		port, err := signalPort(circuit, op.Outputs[0])
		if err != nil {
			return false, err
		}
		if port.Kind != SignalBits {
			return false, fmt.Errorf("%s only supports bit signals in module %s", op.Kind, circuit.Name)
		}
		bits := make([]bool, port.Width)
		if op.Kind == "HIGH" {
			for i := range bits {
				bits[i] = true
			}
		}
		env[port.Name] = Value{Kind: SignalBits, Bits: bits}
		return true, nil

	case "AND", "OR":
		left, leftOk := env[op.Inputs[0]]
		right, rightOk := env[op.Inputs[1]]
		if leftOk && left.Kind != SignalBits {
			leftOk = false
		}
		if rightOk && right.Kind != SignalBits {
			rightOk = false
		}
		if !leftOk && !rightOk {
			return false, nil
		}
		if !leftOk {
			value, applied, err := shortCircuitGate(op, right, circuit)
			if err != nil {
				return false, err
			}
			if !applied {
				return false, nil
			}
			env[op.Outputs[0]] = value
			if err := inferSignalPort(circuit, op.Outputs[0], value); err != nil {
				return false, err
			}
			return true, nil
		}
		if !rightOk {
			value, applied, err := shortCircuitGate(op, left, circuit)
			if err != nil {
				return false, err
			}
			if !applied {
				return false, nil
			}
			env[op.Outputs[0]] = value
			if err := inferSignalPort(circuit, op.Outputs[0], value); err != nil {
				return false, err
			}
			return true, nil
		}
		if len(left.Bits) != len(right.Bits) {
			return false, fmt.Errorf("gate %s in module %s has mismatched input widths", op.Name, circuit.Name)
		}
		result := make([]bool, len(left.Bits))
		for i := range left.Bits {
			if op.Kind == "AND" {
				result[i] = left.Bits[i] && right.Bits[i]
			} else {
				result[i] = left.Bits[i] || right.Bits[i]
			}
		}
		value := Value{Kind: SignalBits, Bits: result}
		env[op.Outputs[0]] = value
		if err := inferSignalPort(circuit, op.Outputs[0], value); err != nil {
			return false, err
		}
		return true, nil

	case "NOT":
		input, ok := env[op.Inputs[0]]
		if !ok || input.Kind != SignalBits {
			return false, nil
		}
		result := make([]bool, len(input.Bits))
		for i := range input.Bits {
			result[i] = !input.Bits[i]
		}
		value := Value{Kind: SignalBits, Bits: result}
		env[op.Outputs[0]] = value
		if err := inferSignalPort(circuit, op.Outputs[0], value); err != nil {
			return false, err
		}
		return true, nil

	case "BUF":
		input, ok := env[op.Inputs[0]]
		if !ok || input.Kind == SignalErr {
			return false, nil
		}
		env[op.Outputs[0]] = cloneValue(input)
		if err := inferSignalPort(circuit, op.Outputs[0], input); err != nil {
			return false, err
		}
		return true, nil

	case "SPLIT":
		source, ok := env[op.Inputs[0]]
		if !ok || source.Kind != SignalBits {
			return false, nil
		}
		if len(source.Bits) != len(op.Outputs) {
			return false, fmt.Errorf("split in module %s expected %d outputs for %s, got %d", circuit.Name, len(source.Bits), op.Inputs[0], len(op.Outputs))
		}
		for i, output := range op.Outputs {
			env[output] = Value{Kind: SignalBits, Bits: []bool{source.Bits[i]}}
		}
		return true, nil

	case "JOIN":
		bits := make([]bool, len(op.Inputs))
		for i, inputName := range op.Inputs {
			input, ok := env[inputName]
			if !ok || input.Kind != SignalBits || len(input.Bits) != 1 {
				return false, nil
			}
			bits[i] = input.Bits[0]
		}
		value := Value{Kind: SignalBits, Bits: bits}
		env[op.Outputs[0]] = value
		if err := inferSignalPort(circuit, op.Outputs[0], value); err != nil {
			return false, err
		}
		return true, nil

	case "USE":
		child, ok := modules[op.Module]
		if !ok {
			return false, fmt.Errorf("module %s not found for %s", op.Module, circuit.Name)
		}
		sources := allSourcePorts(child)
		expectedSignals := len(sources) + len(child.Outputs)
		if len(op.Signals) != expectedSignals {
			return false, fmt.Errorf("module %s instance %s expected %d signals, got %d", child.Name, op.Name, expectedSignals, len(op.Signals))
		}
		childInputs := make(map[string]Value, len(sources))
		for i, in := range sources {
			parentSignal := op.Signals[i]
			value, ok := env[parentSignal]
			if !ok || value.Kind == SignalErr {
				return false, nil
			}
			if err := ensurePortValue(in, value, child.Name); err != nil {
				return false, fmt.Errorf("module %s instance %s using %s: %w", child.Name, op.Name, parentSignal, err)
			}
			childInputs[in.Name] = cloneValue(value)
		}
		childOutputs, err := evaluate(project, child, childInputs, childScope(scope, op))
		if err != nil {
			return false, err
		}
		for i, out := range child.Outputs {
			parentSignal := op.Signals[len(sources)+i]
			value := childOutputs[out.Name]
			env[parentSignal] = cloneValue(value)
			if err := inferSignalPort(circuit, parentSignal, value); err != nil {
				return false, err
			}
		}
		return true, nil
	}

	return false, fmt.Errorf("unknown operation %s in module %s", op.Kind, circuit.Name)
}

func childScope(scope string, op Operation) string {
	return scope + "." + op.Name
}

func shortCircuitGate(op Operation, known Value, circuit *Circuit) (Value, bool, error) {
	if known.Kind != SignalBits {
		return Value{}, false, fmt.Errorf("gate %s in module %s only supports bit signals", op.Name, circuit.Name)
	}
	bits := make([]bool, len(known.Bits))
	switch op.Kind {
	case "AND":
		for _, bit := range known.Bits {
			if bit {
				return Value{}, false, nil
			}
		}
		return Value{Kind: SignalBits, Bits: bits}, true, nil
	case "OR":
		for i, bit := range known.Bits {
			if !bit {
				return Value{}, false, nil
			}
			bits[i] = true
		}
		return Value{Kind: SignalBits, Bits: bits}, true, nil
	default:
		return Value{}, false, nil
	}
}

func unresolvedError(circuit *Circuit, pending []Operation, env map[string]Value) error {
	available := make([]string, 0, len(env))
	for name := range env {
		available = append(available, name)
	}
	sort.Strings(available)

	blocked := make([]string, 0, len(pending))
	for _, op := range pending {
		name := op.Kind
		if op.Name != "" {
			name += " " + op.Name
		}
		if op.Module != "" {
			name += " " + op.Module
		}
		blocked = append(blocked, name)
	}
	return fmt.Errorf("module %s has unresolved signals; available=%v pending=%v", circuit.Name, available, blocked)
}

func inferSignalPort(circuit *Circuit, name string, value Value) error {
	port := portFromValue(name, value)
	return registerSignal(circuit.Signals, port)
}

func signalPort(circuit *Circuit, name string) (Port, error) {
	port, ok := circuit.Signals[name]
	if !ok {
		return Port{}, fmt.Errorf("signal %s has no declared or inferred type in module %s", name, circuit.Name)
	}
	return port, nil
}

type simulationCommandKind string

const (
	simulationCommandShow        simulationCommandKind = "show"
	simulationCommandSet         simulationCommandKind = "set"
	simulationCommandStop        simulationCommandKind = "stop"
	simulationCommandClockAuto   simulationCommandKind = "clock_auto"
	simulationCommandClockManual simulationCommandKind = "clock_manual"
	simulationCommandClockStep   simulationCommandKind = "clock_step"
)

type clockMode string

const (
	clockModeManual clockMode = "manual"
	clockModeAuto   clockMode = "auto"
)

type simulationCommand struct {
	kind        simulationCommandKind
	port        Port
	value       Value
	clockName   string
	frequencyHz float64
	clockSteps  int
}

type clockState struct {
	mode       clockMode
	frequency  float64
	nextToggle time.Time
}

type simulationLine struct {
	text string
	err  error
}

func evaluateAndRenderStep(project *Project, options SimulationOptions, inputs map[string]Value, viewer *displayViewer, output io.Writer) (*displayViewer, error) {
	outputs, err := Evaluate(project, project.Entry, inputs)
	if err != nil {
		return viewer, err
	}
	frames := listDisplayFrames(project)
	if len(frames) > 0 {
		if viewer == nil {
			viewer, err = newDisplayViewer()
			if err == nil {
				fmt.Fprintf(output, "\nDisplay viewer: %s\n", viewer.URL())
			}
		}
		if viewer != nil {
			if err := viewer.Update(frames); err != nil {
				return viewer, err
			}
		}
		if options.DisplayExportPath != "" {
			if err := WriteDisplayFiles(project, options.DisplayExportPath); err != nil {
				return viewer, err
			}
		}
	}

	fmt.Fprintln(output)
	for _, out := range project.Entry.Outputs {
		fmt.Fprintf(output, "%s = %s\n", out.Name, formatValue(outputs[out.Name]))
	}
	return viewer, nil
}

func parseSimulationCommand(text string, declared map[string]Port, clocks map[string]Port) (simulationCommand, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return simulationCommand{}, fmt.Errorf("enter 'set <signal> <value>', 'clock <auto|manual|step> ...', 'show', or 'stop'")
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return simulationCommand{}, fmt.Errorf("enter 'set <signal> <value>', 'clock <auto|manual|step> ...', 'show', or 'stop'")
	}

	switch strings.ToLower(fields[0]) {
	case "show":
		if len(fields) != 1 {
			return simulationCommand{}, fmt.Errorf("show does not take arguments")
		}
		return simulationCommand{kind: simulationCommandShow}, nil
	case "stop", "exit", "quit":
		if len(fields) != 1 {
			return simulationCommand{}, fmt.Errorf("stop does not take arguments")
		}
		return simulationCommand{kind: simulationCommandStop}, nil
	case "set":
		if len(fields) < 2 {
			return simulationCommand{}, fmt.Errorf("usage: set <signal> [<value>]")
		}
		rawValue := ""
		if len(fields) >= 3 {
			rawValue = strings.Join(fields[2:], " ")
		}
		return buildSetCommand(fields[1], rawValue, declared)
	case "clock":
		return parseClockCommand(fields, clocks)
	default:
		if len(fields) < 2 {
			return simulationCommand{}, fmt.Errorf("unknown command %q", fields[0])
		}
		rawValue := strings.Join(fields[1:], " ")
		return buildSetCommand(fields[0], rawValue, declared)
	}
}

func parseClockCommand(fields []string, clocks map[string]Port) (simulationCommand, error) {
	if len(fields) < 2 {
		return simulationCommand{}, fmt.Errorf("usage: clock <auto|manual|step> ...")
	}
	switch strings.ToLower(fields[1]) {
	case "auto":
		if len(fields) != 4 {
			return simulationCommand{}, fmt.Errorf("usage: clock auto <clock> <hz>")
		}
		if _, ok := clocks[fields[2]]; !ok {
			return simulationCommand{}, fmt.Errorf("unknown clock %s", fields[2])
		}
		freq, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			return simulationCommand{}, fmt.Errorf("clock frequency must be a number")
		}
		return simulationCommand{kind: simulationCommandClockAuto, clockName: fields[2], frequencyHz: freq}, nil
	case "manual":
		if len(fields) != 3 {
			return simulationCommand{}, fmt.Errorf("usage: clock manual <clock>")
		}
		if _, ok := clocks[fields[2]]; !ok {
			return simulationCommand{}, fmt.Errorf("unknown clock %s", fields[2])
		}
		return simulationCommand{kind: simulationCommandClockManual, clockName: fields[2]}, nil
	case "step":
		if len(fields) != 4 {
			return simulationCommand{}, fmt.Errorf("usage: clock step <clock> <half|full>")
		}
		if _, ok := clocks[fields[2]]; !ok {
			return simulationCommand{}, fmt.Errorf("unknown clock %s", fields[2])
		}
		steps := 0
		switch strings.ToLower(fields[3]) {
		case "half":
			steps = 1
		case "full":
			steps = 2
		default:
			return simulationCommand{}, fmt.Errorf("clock step must be 'half' or 'full'")
		}
		return simulationCommand{kind: simulationCommandClockStep, clockName: fields[2], clockSteps: steps}, nil
	default:
		return simulationCommand{}, fmt.Errorf("unknown clock command %q", fields[1])
	}
}

func buildSetCommand(name, rawValue string, declared map[string]Port) (simulationCommand, error) {
	port, ok := declared[name]
	if !ok {
		return simulationCommand{}, fmt.Errorf("unknown source signal %s", name)
	}
	if rawValue == "" {
		return simulationCommand{kind: simulationCommandSet, port: port, value: errValue()}, nil
	}
	value, err := parseValue(rawValue, port)
	if err != nil {
		return simulationCommand{}, err
	}
	return simulationCommand{kind: simulationCommandSet, port: port, value: value}, nil
}

func initializeClockStates(clocks []Port) map[string]*clockState {
	states := make(map[string]*clockState, len(clocks))
	for _, clock := range clocks {
		states[clock.Name] = &clockState{mode: clockModeManual}
	}
	return states
}

func setClockAuto(states map[string]*clockState, name string, frequency float64, now time.Time) error {
	state, ok := states[name]
	if !ok {
		return fmt.Errorf("unknown clock %s", name)
	}
	halfPeriod, err := clockHalfPeriod(frequency)
	if err != nil {
		return err
	}
	state.mode = clockModeAuto
	state.frequency = frequency
	state.nextToggle = now.Add(halfPeriod)
	return nil
}

func setClockManual(states map[string]*clockState, name string) error {
	state, ok := states[name]
	if !ok {
		return fmt.Errorf("unknown clock %s", name)
	}
	state.mode = clockModeManual
	state.frequency = 0
	state.nextToggle = time.Time{}
	return nil
}

func stepManualClock(project *Project, options SimulationOptions, inputs map[string]Value, viewer *displayViewer, output io.Writer, states map[string]*clockState, name string, steps int) (*displayViewer, error) {
	state, ok := states[name]
	if !ok {
		return viewer, fmt.Errorf("unknown clock %s", name)
	}
	if state.mode != clockModeManual {
		return viewer, fmt.Errorf("clock %s is in auto mode; switch it to manual before stepping", name)
	}
	for i := 0; i < steps; i++ {
		if err := toggleClockInput(inputs, name); err != nil {
			return viewer, err
		}
		var err error
		viewer, err = evaluateAndRenderStep(project, options, inputs, viewer, output)
		if err != nil {
			return viewer, err
		}
	}
	return viewer, nil
}

func toggleClockInput(inputs map[string]Value, name string) error {
	value, ok := inputs[name]
	if !ok {
		return fmt.Errorf("missing clock input %s", name)
	}
	if value.Kind != SignalBits || len(value.Bits) != 1 {
		return fmt.Errorf("clock %s must be 1 bit", name)
	}
	value.Bits[0] = !value.Bits[0]
	inputs[name] = value
	return nil
}

func clockHalfPeriod(frequency float64) (time.Duration, error) {
	if frequency <= 0 {
		return 0, fmt.Errorf("clock frequency must be greater than 0")
	}
	halfPeriod := float64(time.Second) / (2 * frequency)
	if halfPeriod < 1 {
		halfPeriod = 1
	}
	return time.Duration(halfPeriod), nil
}

func nextAutoClockTimer(states map[string]*clockState, now time.Time) (*time.Timer, <-chan time.Time) {
	var next time.Time
	for _, state := range states {
		if state.mode != clockModeAuto {
			continue
		}
		if next.IsZero() || state.nextToggle.Before(next) {
			next = state.nextToggle
		}
	}
	if next.IsZero() {
		return nil, nil
	}
	delay := time.Until(next)
	if delay < 0 {
		delay = 0
	}
	timer := time.NewTimer(delay)
	return timer, timer.C
}

func stopClockTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func advanceAutoClocks(inputs map[string]Value, states map[string]*clockState, now time.Time) bool {
	changed := false
	for name, state := range states {
		if state.mode != clockModeAuto {
			continue
		}
		halfPeriod, err := clockHalfPeriod(state.frequency)
		if err != nil {
			continue
		}
		for !state.nextToggle.After(now) {
			if err := toggleClockInput(inputs, name); err != nil {
				break
			}
			state.nextToggle = state.nextToggle.Add(halfPeriod)
			changed = true
		}
	}
	return changed
}

func readSimulationLines(reader *bufio.Reader, lines chan<- simulationLine) {
	defer close(lines)
	for {
		text, err := reader.ReadString('\n')
		lines <- simulationLine{text: text, err: err}
		if err != nil {
			return
		}
	}
}

func printSimulationPrompt(output io.Writer) {
	fmt.Fprint(output, "\nCommand (set <signal> <value> | clock <auto|manual|step> ... | show | stop): ")
}

func readInput(reader *bufio.Reader, output io.Writer, port Port) (Value, error) {
	for {
		fmt.Fprint(output, inputPrompt(port))
		text, err := reader.ReadString('\n')
		if err != nil {
			return Value{}, err
		}
		value, err := parseValue(strings.TrimSpace(text), port)
		if err != nil {
			fmt.Fprintf(output, "%v\n", err)
			continue
		}
		return value, nil
	}
}

func inputPrompt(port Port) string {
	switch port.Kind {
	case SignalBits:
		return fmt.Sprintf("%s[%d] (binary, optional): ", port.Name, port.Width)
	case SignalRGB:
		return fmt.Sprintf("%s (r,g,b each 0-255 or 8-bit binary, optional): ", port.Name)
	case SignalBW:
		return fmt.Sprintf("%s (bw 0-255 or 8-bit binary, optional): ", port.Name)
	default:
		return fmt.Sprintf("%s (optional): ", port.Name)
	}
}

func parseValue(text string, port Port) (Value, error) {
	switch port.Kind {
	case SignalBits:
		bits, err := parseBits(text, port.Width)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: SignalBits, Bits: bits}, nil
	case SignalRGB:
		channels, err := parseRGBValue(text)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: SignalRGB, Channels: channels}, nil
	case SignalBW:
		channels, err := parseBWValue(text)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: SignalBW, Channels: channels}, nil
	}
	return Value{}, fmt.Errorf("unsupported signal kind %s", port.Kind)
}

func parseBits(text string, width int) ([]bool, error) {
	if len(text) != width {
		return nil, fmt.Errorf("enter exactly %d bits", width)
	}
	bits := make([]bool, width)
	for i, ch := range text {
		switch ch {
		case '0':
			bits[i] = false
		case '1':
			bits[i] = true
		default:
			return nil, fmt.Errorf("only 0 and 1 are allowed")
		}
	}
	return bits, nil
}

func parseChannels(text string, count int) ([]uint8, error) {
	parts := strings.Split(text, ",")
	if len(parts) != count {
		if count == 1 {
			parts = []string{text}
		} else {
			return nil, fmt.Errorf("enter exactly %d comma-separated values", count)
		}
	}
	channels := make([]uint8, count)
	for i, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value < 0 || value > 255 {
			return nil, fmt.Errorf("channel values must be between 0 and 255")
		}
		channels[i] = uint8(value)
	}
	return channels, nil
}

func parseBWValue(text string) ([]uint8, error) {
	trimmed := strings.TrimSpace(text)
	value, err := parseByteValue(trimmed)
	if err != nil {
		return nil, err
	}
	return []uint8{value}, nil
}

func parseRGBValue(text string) ([]uint8, error) {
	parts := strings.Split(text, ",")
	if len(parts) != 3 {
		return nil, fmt.Errorf("enter exactly 3 comma-separated values")
	}
	channels := make([]uint8, 3)
	for i, part := range parts {
		value, err := parseByteValue(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		channels[i] = value
	}
	return channels, nil
}

func parseByteValue(text string) (uint8, error) {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) == 8 {
		if bits, err := parseBits(trimmed, 8); err == nil {
			value := 0
			for _, bit := range bits {
				value <<= 1
				if bit {
					value |= 1
				}
			}
			return uint8(value), nil
		}
		if strings.IndexFunc(trimmed, func(r rune) bool { return r != '0' && r != '1' }) == -1 {
			return 0, fmt.Errorf("only 0 and 1 are allowed")
		}
	}
	channels, err := parseChannels(trimmed, 1)
	if err != nil {
		return 0, err
	}
	return channels[0], nil
}

func formatValue(value Value) string {
	switch value.Kind {
	case SignalBits:
		var b strings.Builder
		for _, bit := range value.Bits {
			if bit {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
		return b.String()
	case SignalRGB:
		return fmt.Sprintf("%d,%d,%d", value.Channels[0], value.Channels[1], value.Channels[2])
	case SignalBW:
		return fmt.Sprintf("%d", value.Channels[0])
	case SignalErr:
		return "err"
	default:
		return ""
	}
}

func cloneValue(value Value) Value {
	return Value{Kind: value.Kind, Bits: append([]bool(nil), value.Bits...), Channels: append([]uint8(nil), value.Channels...)}
}

func allSourcePorts(circuit *Circuit) []Port {
	ports := make([]Port, 0, len(circuit.Inputs)+len(circuit.Clocks))
	ports = append(ports, circuit.Inputs...)
	ports = append(ports, circuit.Clocks...)
	return ports
}

func ReadInputFile(circuit *Circuit, path string) (map[string]Value, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	ports := allSourcePorts(circuit)
	inputs := make(map[string]Value, len(ports))
	declared := make(map[string]Port, len(ports))
	for _, in := range ports {
		declared[in.Name] = in
	}

	lines := strings.Split(string(data), "\n")
	for lineNo, raw := range lines {
		line := stripComment(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("%s:%d: expected '<signal> <value>'", path, lineNo+1)
		}
		name := fields[0]
		port, ok := declared[name]
		if !ok {
			return nil, fmt.Errorf("%s:%d: unknown input %s", path, lineNo+1, name)
		}
		value, err := parseValue(strings.Join(fields[1:], " "), port)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo+1, err)
		}
		inputs[name] = value
	}

	for _, in := range ports {
		if _, ok := inputs[in.Name]; !ok {
			inputs[in.Name] = errValue()
		}
	}

	return inputs, nil
}

func WriteOutputFile(circuit *Circuit, path string, outputs map[string]Value) error {
	var b strings.Builder
	for _, out := range circuit.Outputs {
		value, ok := outputs[out.Name]
		if !ok {
			return fmt.Errorf("missing output %s", out.Name)
		}
		b.WriteString(out.Name)
		b.WriteByte(' ')
		b.WriteString(formatValue(value))
		b.WriteByte('\n')
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func ensurePortValue(port Port, value Value, moduleName string) error {
	if value.Kind == SignalErr {
		return nil
	}
	if value.Kind != port.Kind {
		return fmt.Errorf("signal %s in module %s expected %s, got %s", port.Name, moduleName, port.Kind, value.Kind)
	}
	if port.Kind == SignalBits && len(value.Bits) != port.Width {
		return fmt.Errorf("signal %s in module %s expected %d bits", port.Name, moduleName, port.Width)
	}
	if port.Kind == SignalRGB && len(value.Channels) != 3 {
		return fmt.Errorf("signal %s in module %s expected rgb pixel", port.Name, moduleName)
	}
	if port.Kind == SignalBW && len(value.Channels) != 1 {
		return fmt.Errorf("signal %s in module %s expected bw pixel", port.Name, moduleName)
	}
	return nil
}

func portFromValue(name string, value Value) Port {
	port := Port{Name: name, Kind: value.Kind}
	if value.Kind == SignalBits {
		port.Width = len(value.Bits)
	}
	return port
}
