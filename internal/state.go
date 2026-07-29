package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func persistentStatePath(entryPath string) string {
	base := strings.TrimSuffix(filepath.Base(entryPath), filepath.Ext(entryPath))
	return filepath.Join(filepath.Dir(entryPath), base+".state")
}

func loadPersistentState(project *Project) error {
	if project.StatePath == "" {
		return nil
	}
	data, err := os.ReadFile(project.StatePath)
	if err != nil {
		if os.IsNotExist(err) {
			project.FloatState = make(map[string]Value)
			return nil
		}
		return err
	}
	if project.FloatState == nil {
		project.FloatState = make(map[string]Value)
	}
	lines := strings.Split(string(data), "\n")
	for lineNo, raw := range lines {
		line := stripComment(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return fmt.Errorf("%s:%d: expected '<floating-path> <0|1>'", project.StatePath, lineNo+1)
		}
		bit, err := strconv.Atoi(fields[1])
		if err != nil || (bit != 0 && bit != 1) {
			return fmt.Errorf("%s:%d: floating gate values must be 0 or 1", project.StatePath, lineNo+1)
		}
		project.FloatState[fields[0]] = Value{Kind: SignalBits, Bits: []bool{bit == 1}}
	}
	return nil
}

func savePersistentState(project *Project) error {
	if project.StatePath == "" {
		return nil
	}
	if project.FloatState == nil {
		project.FloatState = make(map[string]Value)
	}
	keys := make([]string, 0, len(project.FloatState))
	for key := range project.FloatState {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		value := project.FloatState[key]
		bit := 0
		if value.Kind == SignalBits && len(value.Bits) > 0 && value.Bits[0] {
			bit = 1
		}
		b.WriteString(key)
		b.WriteByte(' ')
		b.WriteString(strconv.Itoa(bit))
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(project.StatePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(project.StatePath, []byte(b.String()), 0o644)
}
