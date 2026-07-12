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

		case "NOT", "BUF":
			if len(fields) != 4 {
				return nil, fmt.Errorf("%s:%d: invalid %s", cleanPath, lineNo+1, fields[0])
			}
			circuit.Ops = append(circuit.Ops, Operation{Kind: fields[0], Name: fields[1], Inputs: []string{fields[2]}, Outputs: []string{fields[3]}})

		case "SPLIT":
			if len(fields) < 3 {
				return nil, fmt.Errorf("%s:%d: invalid SPLIT", cleanPath, lineNo+1)
			}
			for _, output := range fields[2:] {
				if err := registerSignal(circuit.Signals, Port{Name: output, Kind: SignalBits, Width: 1}); err != nil {
					return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
				}
			}
			circuit.Ops = append(circuit.Ops, Operation{Kind: "SPLIT", Inputs: []string{fields[1]}, Outputs: append([]string(nil), fields[2:]...)})

		case "JOIN":
			if len(fields) < 4 {
				return nil, fmt.Errorf("%s:%d: invalid JOIN", cleanPath, lineNo+1)
			}
			for _, input := range fields[1 : len(fields)-1] {
				if err := registerSignal(circuit.Signals, Port{Name: input, Kind: SignalBits, Width: 1}); err != nil {
					return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
				}
			}
			output := Port{Name: fields[len(fields)-1], Kind: SignalBits, Width: len(fields) - 2}
			if err := registerSignal(circuit.Signals, output); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, lineNo+1, err)
			}
			circuit.Ops = append(circuit.Ops, Operation{Kind: "JOIN", Inputs: append([]string(nil), fields[1:len(fields)-1]...), Outputs: []string{fields[len(fields)-1]}})

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

func registerSignal(signals map[string]Port, port Port) error {
	if existing, ok := signals[port.Name]; ok {
		if existing.Kind != port.Kind || existing.Width != port.Width {
			return fmt.Errorf("signal %s type mismatch", port.Name)
		}
		return nil
	}
	signals[port.Name] = port
	return nil
}
