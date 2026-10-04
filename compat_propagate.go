package colfer

// propagate widens each node's verdict with the worst verdict reachable
// through nested-struct edges. Iteration terminates because a verdict can
// only widen compatible -> unknown -> incompatible and the node set is finite;
// recursive type graphs (including self-reference cycles) therefore settle at
// a fixed point.
func (ctx *compatContext) propagate() {
	for {
		changed := false
		for _, node := range ctx.nodes {
			for _, edge := range node.edges {
				if worse(edge.to.oldToNew, node.oldToNew) {
					node.oldToNew = edge.to.oldToNew
					changed = true
				}
				if worse(edge.to.newToOld, node.newToOld) {
					node.newToOld = edge.to.newToOld
					changed = true
				}
			}
		}
		if !changed {
			return
		}
	}
}

// worse reports whether candidate is strictly worse than current.
func worse(candidate, current Verdict) bool {
	return rank(candidate) > rank(current)
}

func rank(v Verdict) int {
	switch v {
	case VerdictIncompatible:
		return 2
	case VerdictUnknown:
		return 1
	default:
		return 0
	}
}

// attachPropagated emits one CodeNestedEvolve finding per direction on node for
// each shortest field path that leads to a struct whose own (direct) findings
// prove incompatibility. The traversal is BFS over the reference graph with an
// edge set per node, so recursive graphs terminate.
func (ctx *compatContext) attachPropagated(node *compatNode) {
	if node.oldToNew != VerdictCompatible {
		if path, leaf := ctx.shortestBadPath(node, true); path != nil {
			node.propagated = append(node.propagated, propagatedFinding(node, path, leaf, true))
		}
	}
	if node.newToOld != VerdictCompatible {
		if path, leaf := ctx.shortestBadPath(node, false); path != nil {
			node.propagated = append(node.propagated, propagatedFinding(node, path, leaf, false))
		}
	}
}

// shortestBadPath returns the first edge path (steps named on the traversed
// side) ending at a node carrying a direct verdict for the direction, together
// with that leaf node. It returns nil when the bad verdict is only reachable
// through a cycle with no direct seed.
func (ctx *compatContext) shortestBadPath(start *compatNode, oldToNew bool) ([]PathStep, *compatNode) {
	type frame struct {
		node *compatNode
		path []PathStep
	}
	visited := map[string]bool{start.q: true}
	queue := []frame{{node: start}}

	for len(queue) != 0 {
		cur := queue[0]
		queue = queue[1:]

		for _, edge := range cur.node.edges {
			if visited[edge.to.q] {
				continue
			}
			visited[edge.to.q] = true

			step := edge.stepNew
			if !oldToNew {
				step = edge.stepOld
			}
			path := append(append([]PathStep(nil), cur.path...), step)

			if directVerdict(edge.to, oldToNew) != VerdictCompatible {
				return path, edge.to
			}
			queue = append(queue, frame{node: edge.to, path: path})
		}
	}
	return nil, nil
}

// directVerdict evaluates only the findings declared on the node itself.
func directVerdict(node *compatNode, oldToNew bool) Verdict {
	return findingsVerdict(node.direct, oldToNew)
}

func propagatedFinding(node *compatNode, path []PathStep, leaf *compatNode, oldToNew bool) Finding {
	verdict := VerdictIncompatible
	if directVerdict(leaf, oldToNew) == VerdictUnknown {
		verdict = VerdictUnknown
	}

	f := Finding{
		Code:   CodeNestedEvolve,
		Struct: node.q,
		Path:   path,
		Detail: "a nested struct reached from this type cannot be cross-read; path: " +
			formatPath(node.q, path, leaf.q, oldToNew),
	}
	if oldToNew {
		f.OldToNew = verdict
		f.NewToOld = VerdictCompatible
	} else {
		f.OldToNew = VerdictCompatible
		f.NewToOld = verdict
	}
	return f
}

func formatPath(rootQ string, path []PathStep, leafQ string, oldToNew bool) string {
	side := "new schema"
	if !oldToNew {
		side = "old schema"
	}
	s := rootQ + " (" + side + ")"
	for _, step := range path {
		s += " -> " + step.String()
	}
	s += " -> " + leafQ
	return s
}
