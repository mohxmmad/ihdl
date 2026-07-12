package internal

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func Simulate(project *Project) error {
	reader := bufio.NewReader(os.Stdin)

	for {
		inputs := make(map[string]Value, len(allSourcePorts(project.Entry)))
		for _, in := range allSourcePorts(project.Entry) {
			value, err := readInput(reader, in)
			if err != nil {
				return err
			}
			inputs[in.Name] = value
		}

		outputs, err := Evaluate(project, project.Entry, inputs)
		if err != nil {
			return err
		}

		fmt.Println()
		for _, out := range project.Entry.Outputs {
			fmt.Printf("%s = %s\n", out.Name, formatValue(outputs[out.Name]))
		}

		fmt.Print("\nAgain? (y/n): ")
		answer, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			return nil
		}
		fmt.Println()
	}
}

func SimulateFromFiles(project *Project, inputPath, outputPath string) error {
	inputs, err := ReadInputFile(project.Entry, inputPath)
	if err != nil {
		return err
	}

	outputs, err := Evaluate(project, project.Entry, inputs)
	if err != nil {
		return err
	}

	return WriteOutputFile(project.Entry, outputPath, outputs)
}

func Evaluate(project *Project, circuit *Circuit, inputs map[string]Value) (map[string]Value, error) {
	env := make(map[string]Value)
	for _, in := range allSourcePorts(circuit) {
		value, ok := inputs[in.Name]
		if !ok {
			return nil, fmt.Errorf("missing input %s for module %s", in.Name, circuit.Name)
		}
		if err := ensurePortValue(in, value, circuit.Name); err != nil {
			return nil, err
		}
		env[in.Name] = cloneValue(value)
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
			applied, err := applyOperation(op, env, circuit, modulesByName, project)
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
			return nil, unresolvedError(circuit, next, env)
		}
		pending = next
	}

	outputs := make(map[string]Value, len(circuit.Outputs))
	for _, out := range circuit.Outputs {
		value, ok := env[out.Name]
		if !ok {
			return nil, fmt.Errorf("output %s was never driven in module %s", out.Name, circuit.Name)
		}
		if err := ensurePortValue(out, value, circuit.Name); err != nil {
			return nil, err
		}
		outputs[out.Name] = cloneValue(value)
	}

	return outputs, nil
}

func applyOperation(op Operation, env map[string]Value, circuit *Circuit, modules map[string]*Circuit, project *Project) (bool, error) {
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
		left, ok := env[op.Inputs[0]]
		if !ok {
			return false, nil
		}
		right, ok := env[op.Inputs[1]]
		if !ok {
			return false, nil
		}
		if left.Kind != SignalBits || right.Kind != SignalBits {
			return false, fmt.Errorf("gate %s in module %s only supports bit signals", op.Name, circuit.Name)
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
		if !ok {
			return false, nil
		}
		if input.Kind != SignalBits {
			return false, fmt.Errorf("gate %s in module %s only supports bit signals", op.Name, circuit.Name)
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
		if !ok {
			return false, nil
		}
		env[op.Outputs[0]] = cloneValue(input)
		if err := inferSignalPort(circuit, op.Outputs[0], input); err != nil {
			return false, err
		}
		return true, nil

	case "SPLIT":
		source, ok := env[op.Inputs[0]]
		if !ok {
			return false, nil
		}
		if source.Kind != SignalBits {
			return false, fmt.Errorf("split in module %s only supports bit signals", circuit.Name)
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
			if !ok {
				return false, nil
			}
			if input.Kind != SignalBits || len(input.Bits) != 1 {
				return false, fmt.Errorf("join in module %s only accepts 1-bit inputs", circuit.Name)
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
			if !ok {
				return false, nil
			}
			if err := ensurePortValue(in, value, child.Name); err != nil {
				return false, fmt.Errorf("module %s instance %s using %s: %w", child.Name, op.Name, parentSignal, err)
			}
			childInputs[in.Name] = cloneValue(value)
		}
		childOutputs, err := Evaluate(project, child, childInputs)
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

func readInput(reader *bufio.Reader, port Port) (Value, error) {
	for {
		fmt.Print(inputPrompt(port))
		text, err := reader.ReadString('\n')
		if err != nil {
			return Value{}, err
		}
		value, err := parseValue(strings.TrimSpace(text), port)
		if err != nil {
			fmt.Printf("%v\n", err)
			continue
		}
		return value, nil
	}
}

func inputPrompt(port Port) string {
	switch port.Kind {
	case SignalBits:
		return fmt.Sprintf("%s[%d] (binary): ", port.Name, port.Width)
	case SignalRGB:
		return fmt.Sprintf("%s (r,g,b each 0-255 or 8-bit binary): ", port.Name)
	case SignalBW:
		return fmt.Sprintf("%s (bw 0-255 or 8-bit binary): ", port.Name)
	default:
		return fmt.Sprintf("%s: ", port.Name)
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
			return nil, fmt.Errorf("%s: missing input %s", path, in.Name)
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
