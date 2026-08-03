package internal

// This file implements the compile-time layout used by the compact state
// store.  Instead of keeping every wire of every instance alive forever
// (the old map[string]map[string]Value WireState, which can reach tens of
// gigabytes for deeply nested hierarchies such as icpu's RAM32K), we
// precompute, per module:
//
//   - which wires participate in a feedback cycle and therefore must persist
//     between evaluations (stateWires),
//   - which USE operations can ever receive an "ignore" input, and therefore
//     need their child's outputs cached so a skipped child can hold its last
//     output (skippable),
//   - a flat bit-slot layout so every instance's persistent state lives at a
//     fixed integer offset in one packed array instead of a string-keyed map.
//
// The observable evaluation semantics are unchanged: err values are never
// persisted, ignore inputs still skip child evaluation, and a skipped child
// returns its cached outputs (or its defaults on the very first skip).

// compiledCircuit is the compact-state layout for a single module.
type compiledCircuit struct {
	circuit *Circuit

	// stateWires are the module's bit wires that sit on a feedback cycle and
	// must be persisted between evaluations. stateWidths parallels stateWires
	// and holds each wire's bit width.
	stateWires  []string
	stateWidths []int

	// stateWideNames are stateful wires whose kind is not SignalBits (rare);
	// they are persisted in a small side map instead of the bit array.
	stateWideNames []string
	usesWideState  bool

	// stateOwn is the number of bit slots consumed by stateWires.
	stateOwn int

	// stateOffs is parallel to circuit.Ops. For a USE op it holds the base
	// offset (inside this instance's state region) of the child instance.
	// For any other op it is -1.
	stateOffs []int

	// stateIDs is parallel to circuit.Ops. Every USE op gets a project-wide
	// unique id used as the outCache key, so distinct instances never share a
	// cache slot even when they are stateless (stateTotal 0) and therefore
	// share a state offset. For any non-USE op it is -1.
	stateIDs []int

	// stateTotal is the total number of bit slots required by one instance of
	// this module (own stateWires plus every descendant instance).
	stateTotal int

	// skippable is parallel to circuit.Ops: true when a USE op may receive an
	// ignored input and therefore must cache its child's outputs.
	skippable []bool

	// ctxCanIgnore records, per signal, whether the signal may carry the
	// "ignore" value while this module is being evaluated (i.e. assuming its
	// input ports are non-ignore).
	ctxCanIgnore map[string]bool
}

// compiledProject holds the compact persistent state for a whole project.
type compiledProject struct {
	modules      map[string]*compiledCircuit
	moduleByName map[string]*Circuit

	// stateBits is one byte per stateful bit slot; stateSet is one byte per
	// slot recording whether a persisted value is available.
	stateBits []byte
	stateSet  []byte

	// outCache lazily caches a skippable child instance's outputs. The key is
	// the instance's unique id (stateIDs), so no two instances ever share a
	// cache slot, even when they are stateless and therefore share a state
	// offset. A nil/absent entry means the child has never been evaluated
	// (its outputs default to zero).
	outCache map[int][]Value

	// wideState is a fallback store for non-bit stateful wires.
	wideState map[int]map[string]Value
}

// ensureCompiled builds the compact layout for a project once.
func ensureCompiled(project *Project, circuit *Circuit) error {
	if project.comp == nil {
		cp, err := compileProject(project, circuit)
		if err != nil {
			return err
		}
		project.comp = cp
	}
	return nil
}

// compileProject analyses every module reachable from circuit and allocates
// the flat persistent state array.
func compileProject(project *Project, circuit *Circuit) (*compiledProject, error) {
	moduleByName := make(map[string]*Circuit, len(project.Circuits))
	for _, c := range project.Circuits {
		if c != nil {
			moduleByName[c.Name] = c
		}
	}

	order := moduleTopoOrder(moduleByName, circuit)

	compiled := make(map[string]*compiledCircuit, len(order))
	for _, c := range order {
		names, widths, wideNames := computeStateWires(c, moduleByName)
		own := 0
		for _, w := range widths {
			own += w
		}
		compiled[c.Name] = &compiledCircuit{
			circuit:        c,
			stateWires:     names,
			stateWidths:    widths,
			stateWideNames: wideNames,
			usesWideState:  len(wideNames) > 0,
			stateOwn:       own,
			stateOffs:      make([]int, len(c.Ops)),
			stateIDs:       make([]int, len(c.Ops)),
			skippable:      make([]bool, len(c.Ops)),
		}
	}

	// ctxCanIgnore needs every child's map, so process children before parents.
	for _, c := range order {
		cc := compiled[c.Name]
		cc.ctxCanIgnore = computeCtxCanIgnore(c, moduleByName, compiled)
		cc.skippable = computeSkippable(c, cc, moduleByName)
	}

	nextID := 0
	for _, c := range order {
		computeLayout(c, compiled[c.Name], moduleByName, compiled, &nextID)
	}

	cp := &compiledProject{
		modules:      compiled,
		moduleByName: moduleByName,
		outCache:     make(map[int][]Value),
	}

	entry := compiled[circuit.Name]
	if entry != nil {
		total := entry.stateTotal
		cp.stateBits = make([]byte, total)
		cp.stateSet = make([]byte, total)
	}

	return cp, nil
}

// moduleTopoOrder returns the modules reachable from entry with children
// before parents (post-order DFS). The parser rejects import cycles, so the
// module graph is a DAG and this terminates.
func moduleTopoOrder(moduleByName map[string]*Circuit, entry *Circuit) []*Circuit {
	visited := make(map[string]bool)
	var order []*Circuit
	var visit func(c *Circuit)
	visit = func(c *Circuit) {
		if c == nil || visited[c.Name] {
			return
		}
		visited[c.Name] = true
		for _, op := range c.Ops {
			if op.Kind == "USE" {
				if child := moduleByName[op.Module]; child != nil {
					visit(child)
				}
			}
		}
		order = append(order, c)
	}
	visit(entry)
	return order
}

// computeStateWires finds the module's bit wires that lie on a feedback cycle
// in the signal dependency graph. USE is treated as a black box: every output
// depends on every input, which is a safe over-approximation for cycle
// detection. Non-bit stateful wires are returned separately.
func computeStateWires(c *Circuit, moduleByName map[string]*Circuit) ([]string, []int, []string) {
	deps := make(map[string][]string)
	for _, op := range c.Ops {
		switch op.Kind {
		case "USE":
			child := moduleByName[op.Module]
			if child == nil {
				continue
			}
			n := len(allDeclaredSourcePorts(child))
			if n > len(op.Signals) {
				continue
			}
			for _, out := range op.Signals[n:] {
				deps[out] = append(deps[out], op.Signals[:n]...)
			}
		case "HIGH", "LOW":
			// Produces an output with no input dependencies.
		default:
			for _, out := range op.Outputs {
				deps[out] = append(deps[out], op.Inputs...)
			}
		}
	}

	seen := make(map[string]bool)
	var names []string
	var widths []int
	var wide []string
	consider := func(name string, kind SignalKind, width int) {
		if seen[name] || !reachesSelf(name, deps) {
			return
		}
		seen[name] = true
		if kind == SignalBits {
			names = append(names, name)
			widths = append(widths, width)
		} else {
			wide = append(wide, name)
		}
	}
	// Persist both wires and output ports that sit on a feedback cycle. The
	// legacy WireState map saved every wire and output of every instance, so
	// restoring output ports too keeps hand-built circuits with an output-only
	// feedback loop (e.g. NOT X X) behaving exactly as before.
	for _, w := range c.Wires {
		consider(w.Name, w.Kind, w.Width)
	}
	for _, out := range c.Outputs {
		consider(out.Name, out.Kind, out.Width)
	}
	return names, widths, wide
}

// reachesSelf reports whether signal name can reach itself through the
// dependency edges (i.e. it sits on a cycle).
func reachesSelf(name string, deps map[string][]string) bool {
	visited := make(map[string]bool)
	stack := append([]string(nil), deps[name]...)
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		if cur == name {
			return true
		}
		stack = append(stack, deps[cur]...)
	}
	return false
}

// computeCtxCanIgnore computes, for every signal of the module, whether it can
// carry the "ignore" value while the module is being evaluated (its input
// ports assumed non-ignore). IGNORE produces ignore unconditionally; gates
// propagate it; USE outputs inherit it from the child's own context.
func computeCtxCanIgnore(c *Circuit, moduleByName map[string]*Circuit, compiled map[string]*compiledCircuit) map[string]bool {
	can := make(map[string]bool)
	for {
		changed := false
		for _, op := range c.Ops {
			var prod bool
			switch op.Kind {
			case "IGNORE":
				prod = true
			case "HIGH", "LOW", "PIXEL":
				prod = false
			case "AND", "OR", "NOT", "BUF", "FLOAT", "SPLIT", "JOIN":
				for _, in := range op.Inputs {
					if can[in] {
						prod = true
						break
					}
				}
			case "USE":
				child := moduleByName[op.Module]
				if child == nil {
					break
				}
				n := len(allDeclaredSourcePorts(child))
				childCC := compiled[child.Name]
				for j := n; j < len(op.Signals) && j-n < len(child.Outputs); j++ {
					if childCC != nil && childCC.ctxCanIgnore[child.Outputs[j-n].Name] {
						prod = true
						break
					}
				}
				for _, out := range op.Signals[n:] {
					if prod && !can[out] {
						can[out] = true
						changed = true
					}
				}
				continue
			}
			for _, out := range op.Outputs {
				if prod && !can[out] {
					can[out] = true
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return can
}

// computeSkippable marks USE operations that can receive an ignored input.
func computeSkippable(c *Circuit, cc *compiledCircuit, moduleByName map[string]*Circuit) []bool {
	sk := make([]bool, len(c.Ops))
	for i, op := range c.Ops {
		if op.Kind != "USE" {
			continue
		}
		child := moduleByName[op.Module]
		if child == nil {
			continue
		}
		n := len(allDeclaredSourcePorts(child))
		for j := 0; j < n && j < len(op.Signals); j++ {
			if cc.ctxCanIgnore[op.Signals[j]] {
				sk[i] = true
				break
			}
		}
	}
	return sk
}

// computeLayout fills stateOffs and stateTotal for a module. Children must be
// laid out before parents, which the post-order traversal guarantees. nextID
// accumulates a project-wide unique id per USE op, used as the outCache key.
func computeLayout(c *Circuit, cc *compiledCircuit, moduleByName map[string]*Circuit, compiled map[string]*compiledCircuit, nextID *int) int {
	total := cc.stateOwn
	for i, op := range c.Ops {
		if op.Kind != "USE" {
			cc.stateOffs[i] = -1
			cc.stateIDs[i] = -1
			continue
		}
		cc.stateIDs[i] = *nextID
		*nextID++
		child := moduleByName[op.Module]
		if child == nil {
			cc.stateOffs[i] = -1
			continue
		}
		childCC := compiled[child.Name]
		if childCC == nil {
			cc.stateOffs[i] = -1
			continue
		}
		cc.stateOffs[i] = total
		total += childCC.stateTotal
	}
	cc.stateTotal = total
	return total
}

// loadStateInto restores the instance's persistent stateful wires into env.
// Wires are only restored when a value was persisted (matching the legacy
// behaviour of never saving err values), and never override existing inputs.
func loadStateInto(project *Project, cc *compiledCircuit, stateBase int, env map[string]Value) {
	cp := project.comp
	off := stateBase
	if off < 0 || off+cc.stateOwn > len(cp.stateBits) {
		return
	}
	for k, name := range cc.stateWires {
		w := cc.stateWidths[k]
		if cp.stateSet[off] == 1 {
			bits := make([]bool, w)
			for b := 0; b < w; b++ {
				bits[b] = cp.stateBits[off+b] == 1
			}
			if _, exists := env[name]; !exists {
				env[name] = Value{Kind: SignalBits, Bits: bits}
			}
		}
		off += w
	}
	if cc.usesWideState && cp.wideState != nil {
		if ws, ok := cp.wideState[stateBase]; ok {
			for _, name := range cc.stateWideNames {
				if v, ok := ws[name]; ok {
					if _, exists := env[name]; !exists {
						env[name] = cloneValue(v)
					}
				}
			}
		}
	}
}

// saveStateFrom persists the instance's stateful wires. Only non-err values
// are written; a wire whose value is unavailable simply keeps its previous
// persisted value, exactly like the legacy WireState map.
func saveStateFrom(project *Project, cc *compiledCircuit, stateBase int, env map[string]Value) {
	cp := project.comp
	off := stateBase
	if off < 0 || off+cc.stateOwn > len(cp.stateBits) {
		return
	}
	for k, name := range cc.stateWires {
		w := cc.stateWidths[k]
		if v, ok := env[name]; ok && v.Kind == SignalBits && len(v.Bits) == w {
			for b := 0; b < w; b++ {
				if v.Bits[b] {
					cp.stateBits[off+b] = 1
				} else {
					cp.stateBits[off+b] = 0
				}
				cp.stateSet[off+b] = 1
			}
		}
		off += w
	}
	if cc.usesWideState {
		for _, name := range cc.stateWideNames {
			if v, ok := env[name]; ok && v.Kind != SignalErr {
				if cp.wideState == nil {
					cp.wideState = make(map[int]map[string]Value)
				}
				ws := cp.wideState[stateBase]
				if ws == nil {
					ws = make(map[string]Value)
				}
				ws[name] = cloneValue(v)
				cp.wideState[stateBase] = ws
			}
		}
	}
}
