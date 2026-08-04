package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func ParseProject(entryPath string) (*Project, error) {
	absEntry, err := filepath.Abs(entryPath)
	if err != nil {
		return nil, err
	}
	workspaceRoot, err := findWorkspaceRoot(filepath.Dir(absEntry))
	if err != nil {
		return nil, err
	}
	moduleIndex, err := indexModules(workspaceRoot)
	if err != nil {
		return nil, err
	}

	project := &Project{Circuits: make(map[string]*Circuit)}
	loading := make(map[string]bool)

	entry, err := loadCircuit(absEntry, project.Circuits, loading, moduleIndex)
	if err != nil {
		return nil, err
	}

	project.Entry = entry
	project.EntryPath = absEntry
	project.StatePath = persistentStatePath(absEntry)
	if err := loadPersistentState(project); err != nil {
		return nil, err
	}
	return project, nil
}

func findWorkspaceRoot(start string) (string, error) {
	dir := filepath.Clean(start)
	for {
		if exists(filepath.Join(dir, "go.mod")) || exists(filepath.Join(dir, ".git")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start, nil
		}
		dir = parent
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func loadCircuit(path string, registry map[string]*Circuit, loading map[string]bool, moduleIndex map[string]string) (*Circuit, error) {
	cleanPath := filepath.Clean(path)
	if circuit, ok := registry[cleanPath]; ok {
		return circuit, nil
	}
	if loading[cleanPath] {
		return nil, fmt.Errorf("import cycle detected at %s", cleanPath)
	}

	loading[cleanPath] = true
	defer delete(loading, cleanPath)

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}

	circuit := &Circuit{
		Name:    strings.TrimSuffix(filepath.Base(cleanPath), filepath.Ext(cleanPath)),
		Path:    cleanPath,
		Signals: make(map[string]Port),
	}
	importedModules := make(map[string]bool)
	displayIndex := make(map[string]int)
	buttonAliases := make(map[string]string)

	lines := strings.Split(string(data), "\n")
	for lineNo, raw := range lines {
		line := stripComment(raw)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		switch fields[0] {
		case "MODULE":
			if len(fields) != 2 {
				return nil, fmt.Errorf("%s:%d: invalid MODULE", cleanPath, lineNo+1)
			}
			circuit.Name = fields[1]

		case "IMPORT":
			if len(fields) != 2 {
				return nil, fmt.Errorf("%s:%d: invalid IMPORT", cleanPath, lineNo+1)
			}
			moduleName := fields[1]
			if _, ok := moduleIndex[moduleName]; !ok {
				return nil, fmt.Errorf("%s:%d: unknown imported module %s", cleanPath, lineNo+1, moduleName)
			}
			circuit.Imports = append(circuit.Imports, moduleName)
			importedModules[moduleName] = true

		case "INPUT", "CLOCK", "OUTPUT", "WIRE", "INPUT_RGB", "OUTPUT_RGB", "WIRE_RGB", "INPUT_BW", "OUTPUT_BW", "WIRE_BW":
			if len(fields) != 2 {
				return nil, fmt.Errorf("%s:%d: invalid %s", cleanPath, lineNo+1, fields[0])
			}
			port, target, err := parseDeclaration(fields[0], fields[1])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			if _, ok := displayIndex[port.Name]; ok {
				return nil, fmt.Errorf("%s:%d: signal %s conflicts with display name", cleanPath, lineNo+1, port.Name)
			}
			if err := registerSignal(circuit.Signals, port); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			switch target {
			case "input":
				circuit.Inputs = append(circuit.Inputs, port)
			case "clock":
				circuit.Clocks = append(circuit.Clocks, port)
			case "output":
				circuit.Outputs = append(circuit.Outputs, port)
			case "wire":
				circuit.Wires = append(circuit.Wires, port)
			}

		case "INPUT_GRID", "OUTPUT_GRID", "WIRE_GRID":
			if len(fields) != 4 {
				return nil, fmt.Errorf("%s:%d: invalid %s", cleanPath, lineNo+1, fields[0])
			}
			port, target, err := parseGridDeclaration(fields[0], fields[1], fields[2], fields[3])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			if _, ok := displayIndex[port.Name]; ok {
				return nil, fmt.Errorf("%s:%d: signal %s conflicts with display name", cleanPath, lineNo+1, port.Name)
			}
			if err := registerSignal(circuit.Signals, port); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			switch target {
			case "input":
				circuit.Inputs = append(circuit.Inputs, port)
			case "output":
				circuit.Outputs = append(circuit.Outputs, port)
			case "wire":
				circuit.Wires = append(circuit.Wires, port)
			}

		case "BUTTON":
			button, port, err := parseButton(fields)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			if _, ok := circuit.Signals[port.Name]; ok {
				return nil, fmt.Errorf("%s:%d: button %s conflicts with existing signal name", cleanPath, lineNo+1, port.Name)
			}
			if _, ok := displayIndex[port.Name]; ok {
				return nil, fmt.Errorf("%s:%d: signal %s conflicts with display name", cleanPath, lineNo+1, port.Name)
			}
			if err := registerSignal(circuit.Signals, port); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			aliases := []string{normalizeButtonKey(button.Name), button.Key}
			seen := make(map[string]struct{}, len(aliases))
			for _, alias := range aliases {
				if _, ok := seen[alias]; ok {
					continue
				}
				seen[alias] = struct{}{}
				if existing, ok := circuit.Signals[alias]; ok && existing.Name != button.Name {
					return nil, fmt.Errorf("%s:%d: button shortcut %s conflicts with existing signal name %s", cleanPath, lineNo+1, alias, existing.Name)
				}
				if existing, ok := buttonAliases[alias]; ok && existing != button.Name {
					return nil, fmt.Errorf("%s:%d: button shortcut %s already used by %s", cleanPath, lineNo+1, alias, existing)
				}
			}
			for alias := range seen {
				buttonAliases[alias] = button.Name
			}
			circuit.Inputs = append(circuit.Inputs, port)
			circuit.Buttons = append(circuit.Buttons, button)

		case "DISPLAY":
			if len(fields) != 4 {
				return nil, fmt.Errorf("%s:%d: invalid DISPLAY", cleanPath, lineNo+1)
			}
			display, err := parseDisplay(fields[1], fields[2], fields[3])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			if _, ok := circuit.Signals[display.Name]; ok {
				return nil, fmt.Errorf("%s:%d: display %s conflicts with signal name", cleanPath, lineNo+1, display.Name)
			}
			if _, ok := displayIndex[display.Name]; ok {
				return nil, fmt.Errorf("%s:%d: duplicate display %s", cleanPath, lineNo+1, display.Name)
			}
			displayIndex[display.Name] = len(circuit.Displays)
			circuit.Displays = append(circuit.Displays, display)

		case "PIXEL":
			if len(fields) != 5 {
				return nil, fmt.Errorf("%s:%d: invalid PIXEL", cleanPath, lineNo+1)
			}
			pixel, err := parseDisplayPixel(fields[2], fields[3], fields[4])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			if displayPos, ok := displayIndex[fields[1]]; ok {
				display := &circuit.Displays[displayPos]
				if pixel.X >= display.Width || pixel.Y >= display.Height {
					return nil, fmt.Errorf("%s:%d: pixel (%d,%d) is outside display %s bounds %dx%d", cleanPath, lineNo+1, pixel.X, pixel.Y, display.Name, display.Width, display.Height)
				}
				for _, existing := range display.Pixels {
					if existing.X == pixel.X && existing.Y == pixel.Y {
						return nil, fmt.Errorf("%s:%d: duplicate pixel mapping for %s at (%d,%d)", cleanPath, lineNo+1, display.Name, pixel.X, pixel.Y)
					}
				}
				display.Pixels = append(display.Pixels, pixel)
				continue
			}
			port, ok := circuit.Signals[fields[1]]
			if !ok || port.Kind != SignalGrid {
				return nil, fmt.Errorf("%s:%d: unknown display or grid %s", cleanPath, lineNo+1, fields[1])
			}
			if pixel.X >= port.GridW || pixel.Y >= port.GridH {
				return nil, fmt.Errorf("%s:%d: pixel (%d,%d) is outside grid %s bounds %dx%d", cleanPath, lineNo+1, pixel.X, pixel.Y, port.Name, port.GridW, port.GridH)
			}
			for _, op := range circuit.Ops {
				if op.Kind == "PIXEL" && op.Outputs[0] == port.Name && op.X == pixel.X && op.Y == pixel.Y {
					return nil, fmt.Errorf("%s:%d: duplicate pixel mapping for %s at (%d,%d)", cleanPath, lineNo+1, port.Name, pixel.X, pixel.Y)
				}
			}
			circuit.Ops = append(circuit.Ops, Operation{Kind: "PIXEL", Inputs: []string{pixel.Signal}, Outputs: []string{port.Name}, X: pixel.X, Y: pixel.Y})

		case "GRID":
			if len(fields) != 3 && len(fields) != 5 {
				return nil, fmt.Errorf("%s:%d: invalid GRID", cleanPath, lineNo+1)
			}
			gridPort, ok := circuit.Signals[fields[1]]
			if !ok || gridPort.Kind != SignalGrid {
				return nil, fmt.Errorf("%s:%d: unknown grid %s", cleanPath, lineNo+1, fields[1])
			}
			x, y := 0, 0
			if len(fields) == 5 {
				x, err = strconv.Atoi(fields[3])
				if err != nil || x < 0 {
					return nil, fmt.Errorf("%s:%d: invalid grid x coordinate %q", cleanPath, lineNo+1, fields[3])
				}
				y, err = strconv.Atoi(fields[4])
				if err != nil || y < 0 {
					return nil, fmt.Errorf("%s:%d: invalid grid y coordinate %q", cleanPath, lineNo+1, fields[4])
				}
			}
			if displayPos, ok := displayIndex[fields[2]]; ok {
				display := &circuit.Displays[displayPos]
				if x+gridPort.GridW > display.Width || y+gridPort.GridH > display.Height {
					return nil, fmt.Errorf("%s:%d: grid %s at (%d,%d) exceeds display %s bounds %dx%d", cleanPath, lineNo+1, gridPort.Name, x, y, display.Name, display.Width, display.Height)
				}
				for _, placement := range display.Grids {
					if placement.GridName == gridPort.Name && placement.X == x && placement.Y == y {
						return nil, fmt.Errorf("%s:%d: duplicate grid placement for %s on %s at (%d,%d)", cleanPath, lineNo+1, gridPort.Name, display.Name, x, y)
					}
				}
				display.Grids = append(display.Grids, DisplayGrid{GridName: gridPort.Name, X: x, Y: y})
				continue
			}
			targetPort, ok := circuit.Signals[fields[2]]
			if !ok || targetPort.Kind != SignalGrid {
				return nil, fmt.Errorf("%s:%d: unknown display or grid %s", cleanPath, lineNo+1, fields[2])
			}
			if x+gridPort.GridW > targetPort.GridW || y+gridPort.GridH > targetPort.GridH {
				return nil, fmt.Errorf("%s:%d: grid %s at (%d,%d) exceeds grid %s bounds %dx%d", cleanPath, lineNo+1, gridPort.Name, x, y, targetPort.Name, targetPort.GridW, targetPort.GridH)
			}
			for _, op := range circuit.Ops {
				if op.Kind == "GRID" && op.Outputs[0] == targetPort.Name && op.Inputs[0] == gridPort.Name && op.X == x && op.Y == y {
					return nil, fmt.Errorf("%s:%d: duplicate grid blit of %s into %s at (%d,%d)", cleanPath, lineNo+1, gridPort.Name, targetPort.Name, x, y)
				}
			}
			circuit.Ops = append(circuit.Ops, Operation{Kind: "GRID", Inputs: []string{gridPort.Name}, Outputs: []string{targetPort.Name}, X: x, Y: y})

		case "HIGH", "LOW":
			if len(fields) != 2 {
				return nil, fmt.Errorf("%s:%d: invalid %s", cleanPath, lineNo+1, fields[0])
			}
			port, err := parseSignalRef(fields[1], circuit.Signals)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			if port.Kind != SignalBits {
				return nil, fmt.Errorf("%s:%d: %s only supports bit signals", cleanPath, lineNo+1, fields[0])
			}
			if err := registerSignal(circuit.Signals, port); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			circuit.Ops = append(circuit.Ops, Operation{Kind: fields[0], Outputs: []string{port.Name}})

		case "AND", "OR":
			if len(fields) != 5 {
				return nil, fmt.Errorf("%s:%d: invalid %s", cleanPath, lineNo+1, fields[0])
			}
			circuit.Ops = append(circuit.Ops, Operation{Kind: fields[0], Name: fields[1], Inputs: []string{fields[2], fields[3]}, Outputs: []string{fields[4]}})

		case "NOT", "BUF", "IGNORE":
			if len(fields) != 4 {
				return nil, fmt.Errorf("%s:%d: invalid %s", cleanPath, lineNo+1, fields[0])
			}
			circuit.Ops = append(circuit.Ops, Operation{Kind: fields[0], Name: fields[1], Inputs: []string{fields[2]}, Outputs: []string{fields[3]}})

		case "FLOAT":
			if len(fields) != 5 {
				return nil, fmt.Errorf("%s:%d: invalid FLOAT, want FLOAT <name> <in> <load> <out>", cleanPath, lineNo+1)
			}
			if err := registerSignal(circuit.Signals, Port{Name: fields[2], Kind: SignalBits, Width: 1}); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			if err := registerSignal(circuit.Signals, Port{Name: fields[3], Kind: SignalBits, Width: 1}); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			if err := registerSignal(circuit.Signals, Port{Name: fields[4], Kind: SignalBits, Width: 1}); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			circuit.Ops = append(circuit.Ops, Operation{Kind: "FLOAT", Name: fields[1], Inputs: []string{fields[2], fields[3]}, Outputs: []string{fields[4]}})

		case "SPLIT":
			if len(fields) < 3 {
				return nil, fmt.Errorf("%s:%d: invalid SPLIT", cleanPath, lineNo+1)
			}
			outputWidth, err := inferSplitOutputWidth(circuit.Signals[fields[1]], fields[2:], circuit.Signals)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			for _, output := range fields[2:] {
				if err := registerSignal(circuit.Signals, Port{Name: output, Kind: SignalBits, Width: outputWidth}); err != nil {
					return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
				}
			}
			circuit.Ops = append(circuit.Ops, Operation{Kind: "SPLIT", Inputs: []string{fields[1]}, Outputs: append([]string(nil), fields[2:]...)})

		case "JOIN":
			if len(fields) < 4 {
				return nil, fmt.Errorf("%s:%d: invalid JOIN", cleanPath, lineNo+1)
			}
			outputName := fields[len(fields)-1]
			outputPort, outputExists := circuit.Signals[outputName]
			if outputExists && (outputPort.Kind == SignalBW || outputPort.Kind == SignalRGB) {
				if len(fields)-2 != portChannelCount(outputPort.Kind) {
					return nil, fmt.Errorf("%s:%d: join to %s pixel needs exactly %d inputs", cleanPath, lineNo+1, outputPort.Kind, portChannelCount(outputPort.Kind))
				}
				for _, input := range fields[1 : len(fields)-1] {
					if err := registerSignal(circuit.Signals, Port{Name: input, Kind: SignalBits, Width: 8}); err != nil {
						return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
					}
				}
			} else {
				inputPorts, inferredOutputPort, err := inferJoinBitWidths(fields[1:len(fields)-1], circuit.Signals[outputName], circuit.Signals)
				if err != nil {
					return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
				}
				outputPort = inferredOutputPort
				for i, input := range fields[1 : len(fields)-1] {
					if err := registerSignal(circuit.Signals, Port{Name: input, Kind: SignalBits, Width: inputPorts[i]}); err != nil {
						return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
					}
				}
			}
			if err := registerSignal(circuit.Signals, outputPort); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			circuit.Ops = append(circuit.Ops, Operation{Kind: "JOIN", Inputs: append([]string(nil), fields[1:len(fields)-1]...), Outputs: []string{outputName}})

		case "USE":
			if len(fields) < 4 {
				return nil, fmt.Errorf("%s:%d: invalid USE", cleanPath, lineNo+1)
			}
			if !importedModules[fields[1]] {
				return nil, fmt.Errorf("%s:%d: module %s must be imported before use", cleanPath, lineNo+1, fields[1])
			}
			circuit.Ops = append(circuit.Ops, Operation{Kind: "USE", Name: fields[2], Module: fields[1], Signals: append([]string(nil), fields[3:]...)})

		default:
			if len(fields) >= 3 && importedModules[fields[0]] {
				circuit.Ops = append(circuit.Ops, Operation{Kind: "USE", Name: fields[1], Module: fields[0], Signals: append([]string(nil), fields[2:]...)})
				continue
			}
			return nil, fmt.Errorf("%s:%d: unknown keyword %s", cleanPath, lineNo+1, fields[0])
		}
	}

	registry[cleanPath] = circuit
	for _, moduleName := range circuit.Imports {
		if moduleName == circuit.Name {
			return nil, fmt.Errorf("%s: module %s cannot import itself", cleanPath, moduleName)
		}
		importPath := moduleIndex[moduleName]
		if _, err := loadCircuit(importPath, registry, loading, moduleIndex); err != nil {
			return nil, err
		}
	}

	return circuit, nil
}

func indexModules(root string) (map[string]string, error) {
	index := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".ihdl" {
			return nil
		}
		moduleName, err := readModuleName(path)
		if err != nil {
			return err
		}
		if existing, ok := index[moduleName]; ok && existing != path {
			return fmt.Errorf("duplicate module name %s in %s and %s", moduleName, existing, path)
		}
		index[moduleName] = path
		return nil
	})
	if err != nil {
		return nil, err
	}
	return index, nil
}

func readModuleName(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	for lineNo, raw := range lines {
		line := stripComment(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "MODULE" {
			return fields[1], nil
		}
		return "", fmt.Errorf("%s:%d: first non-empty line must declare MODULE", path, lineNo+1)
	}
	return "", fmt.Errorf("%s: missing MODULE declaration", path)
}

func stripComment(line string) string {
	if idx := strings.Index(line, "#"); idx >= 0 {
		line = line[:idx]
	}
	return strings.TrimSpace(line)
}

func parseDeclaration(keyword, token string) (Port, string, error) {
	switch keyword {
	case "INPUT":
		port, err := parseBitPortToken(token)
		return port, "input", err
	case "CLOCK":
		port, err := parseBitPortToken(token)
		if err != nil {
			return Port{}, "", err
		}
		if port.Width != 1 {
			return Port{}, "", fmt.Errorf("CLOCK %s must be 1 bit", port.Name)
		}
		return port, "clock", nil
	case "OUTPUT":
		port, err := parseBitPortToken(token)
		return port, "output", err
	case "WIRE":
		port, err := parseBitPortToken(token)
		return port, "wire", err
	case "INPUT_RGB":
		port, err := parseNamedPort(token, SignalRGB)
		return port, "input", err
	case "OUTPUT_RGB":
		port, err := parseNamedPort(token, SignalRGB)
		return port, "output", err
	case "WIRE_RGB":
		port, err := parseNamedPort(token, SignalRGB)
		return port, "wire", err
	case "INPUT_BW":
		port, err := parseNamedPort(token, SignalBW)
		return port, "input", err
	case "OUTPUT_BW":
		port, err := parseNamedPort(token, SignalBW)
		return port, "output", err
	case "WIRE_BW":
		port, err := parseNamedPort(token, SignalBW)
		return port, "wire", err
	}
	return Port{}, "", fmt.Errorf("unsupported declaration %s", keyword)
}

func parseBitPortToken(token string) (Port, error) {
	open := strings.IndexByte(token, '[')
	if open == -1 {
		return Port{Name: token, Kind: SignalBits, Width: 1}, nil
	}
	if !strings.HasSuffix(token, "]") {
		return Port{}, fmt.Errorf("invalid signal %q", token)
	}
	name := token[:open]
	widthText := token[open+1 : len(token)-1]
	width, err := strconv.Atoi(widthText)
	if err != nil || width < 1 {
		return Port{}, fmt.Errorf("invalid width in %q", token)
	}
	if name == "" {
		return Port{}, fmt.Errorf("invalid signal %q", token)
	}
	return Port{Name: name, Kind: SignalBits, Width: width}, nil
}

func parseNamedPort(token string, kind SignalKind) (Port, error) {
	if token == "" || strings.Contains(token, "[") || strings.Contains(token, "]") {
		return Port{}, fmt.Errorf("invalid signal %q", token)
	}
	return Port{Name: token, Kind: kind, Width: 0}, nil
}

func parseGridDeclaration(keyword, name, widthText, heightText string) (Port, string, error) {
	if name == "" || strings.Contains(name, "[") || strings.Contains(name, "]") {
		return Port{}, "", fmt.Errorf("invalid signal %q", name)
	}
	width, err := strconv.Atoi(widthText)
	if err != nil || width < 1 {
		return Port{}, "", fmt.Errorf("invalid grid width %q", widthText)
	}
	height, err := strconv.Atoi(heightText)
	if err != nil || height < 1 {
		return Port{}, "", fmt.Errorf("invalid grid height %q", heightText)
	}
	port := Port{Name: name, Kind: SignalGrid, GridW: width, GridH: height}
	switch keyword {
	case "INPUT_GRID":
		return port, "input", nil
	case "OUTPUT_GRID":
		return port, "output", nil
	case "WIRE_GRID":
		return port, "wire", nil
	default:
		return Port{}, "", fmt.Errorf("unsupported declaration %s", keyword)
	}
}

func parseButton(fields []string) (Button, Port, error) {
	if len(fields) != 3 && len(fields) != 4 {
		return Button{}, Port{}, fmt.Errorf("invalid BUTTON")
	}
	name := fields[1]
	key := name
	valueText := fields[2]
	if len(fields) == 4 {
		key = fields[2]
		valueText = fields[3]
	}
	if name == "" || strings.Contains(name, "[") || strings.Contains(name, "]") {
		return Button{}, Port{}, fmt.Errorf("invalid signal %q", name)
	}
	bits, err := parseBits(valueText, 8)
	if err != nil {
		return Button{}, Port{}, fmt.Errorf("button values must be exactly 8 bits: %w", err)
	}
	button := Button{Name: name, Key: normalizeButtonKey(key), Value: bits}
	port := Port{Name: name, Kind: SignalBits, Width: 8}
	return button, port, nil
}

func normalizeButtonKey(key string) string {
	trimmed := strings.TrimSpace(key)
	trimmed = strings.Trim(trimmed, "'")
	trimmed = strings.Trim(trimmed, "\"")
	return strings.ToUpper(trimmed)
}

func parseDisplay(name, widthText, heightText string) (Display, error) {
	if name == "" || strings.Contains(name, "[") || strings.Contains(name, "]") {
		return Display{}, fmt.Errorf("invalid display %q", name)
	}
	width, err := strconv.Atoi(widthText)
	if err != nil || width < 1 {
		return Display{}, fmt.Errorf("invalid display width %q", widthText)
	}
	height, err := strconv.Atoi(heightText)
	if err != nil || height < 1 {
		return Display{}, fmt.Errorf("invalid display height %q", heightText)
	}
	return Display{Name: name, Width: width, Height: height}, nil
}

func parseDisplayPixel(xText, yText, signal string) (DisplayPixel, error) {
	x, err := strconv.Atoi(xText)
	if err != nil || x < 0 {
		return DisplayPixel{}, fmt.Errorf("invalid pixel x coordinate %q", xText)
	}
	y, err := strconv.Atoi(yText)
	if err != nil || y < 0 {
		return DisplayPixel{}, fmt.Errorf("invalid pixel y coordinate %q", yText)
	}
	if signal == "" {
		return DisplayPixel{}, fmt.Errorf("pixel signal cannot be empty")
	}
	return DisplayPixel{X: x, Y: y, Signal: signal}, nil
}

func parseSignalRef(token string, known map[string]Port) (Port, error) {
	port, err := parseBitPortToken(token)
	if err != nil {
		return Port{}, err
	}
	if !strings.Contains(token, "[") {
		if existing, ok := known[port.Name]; ok {
			port = existing
		}
	}
	return port, nil
}

func inferSplitOutputWidth(source Port, outputs []string, signals map[string]Port) (int, error) {
	if len(outputs) == 0 {
		return 0, fmt.Errorf("split needs at least one output")
	}
	if source.Kind == SignalBW || source.Kind == SignalRGB {
		if len(outputs) != portChannelCount(source.Kind) {
			return 0, fmt.Errorf("split on %s pixel needs exactly %d outputs", source.Kind, portChannelCount(source.Kind))
		}
		return 8, nil
	}
	if source.Kind != SignalBits {
		return 1, nil
	}
	declaredWidth := 0
	for _, output := range outputs {
		if existing, ok := signals[output]; ok {
			if existing.Kind != SignalBits {
				return 0, fmt.Errorf("signal %s type mismatch", output)
			}
			if declaredWidth == 0 {
				declaredWidth = existing.Width
			} else if existing.Width != declaredWidth {
				return 0, fmt.Errorf("split outputs must all have the same bit width")
			}
		}
	}
	if declaredWidth > 0 {
		if source.Width > 0 && source.Width != declaredWidth*len(outputs) {
			return 0, fmt.Errorf("split width mismatch: source has %d bits but outputs need %d", source.Width, declaredWidth*len(outputs))
		}
		return declaredWidth, nil
	}
	if source.Width%len(outputs) != 0 {
		return 0, fmt.Errorf("split width mismatch: source has %d bits but %d outputs do not divide evenly", source.Width, len(outputs))
	}
	return source.Width / len(outputs), nil
}

func inferJoinBitWidths(inputs []string, output Port, signals map[string]Port) ([]int, Port, error) {
	widths := make([]int, len(inputs))
	unknown := make([]int, 0, len(inputs))
	knownSum := 0
	for i, input := range inputs {
		if existing, ok := signals[input]; ok {
			if existing.Kind != SignalBits {
				return nil, Port{}, fmt.Errorf("signal %s type mismatch", input)
			}
			widths[i] = existing.Width
			knownSum += existing.Width
		} else {
			unknown = append(unknown, i)
		}
	}
	if output.Kind == SignalBits && output.Width > 0 {
		if len(unknown) == 0 {
			if knownSum != output.Width {
				return nil, Port{}, fmt.Errorf("join width mismatch: inputs total %d bits but output has %d", knownSum, output.Width)
			}
			return widths, output, nil
		}
		remaining := output.Width - knownSum
		if remaining < 0 || remaining%len(unknown) != 0 {
			return nil, Port{}, fmt.Errorf("join width mismatch: output has %d bits but inputs do not fit", output.Width)
		}
		inferred := remaining / len(unknown)
		for _, idx := range unknown {
			widths[idx] = inferred
		}
		return widths, output, nil
	}
	for _, idx := range unknown {
		widths[idx] = 1
	}
	output.Width = 0
	for _, width := range widths {
		output.Width += width
	}
	output.Kind = SignalBits
	return widths, output, nil
}

func registerSignal(signals map[string]Port, port Port) error {
	if existing, ok := signals[port.Name]; ok {
		if existing.Kind != port.Kind || existing.Width != port.Width || existing.GridW != port.GridW || existing.GridH != port.GridH {
			return fmt.Errorf("signal %s type mismatch", port.Name)
		}
		return nil
	}
	signals[port.Name] = port
	return nil
}
