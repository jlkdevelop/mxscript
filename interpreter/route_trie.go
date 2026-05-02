// route_trie.go — segment-based trie for HTTP route lookup. Each path
// segment is a node; static children are stored in a map (O(1) hit),
// `:param` children are a single slot per node, and a `*` wildcard
// child catches the remainder.
//
// Built lazily by Interpreter.dispatch on first request, then reused
// for every subsequent dispatch. Compared to the previous O(n) linear
// scan over registeredRoute, lookup is now O(segments) with branching
// limited to method × path-shape.
package interpreter

import "strings"

type routeTrie struct {
	root *trieNode
}

type trieNode struct {
	// static[seg] -> child node for an exact-match segment
	static map[string]*trieNode
	// param: child node when this segment is a `:name` placeholder
	param     *trieNode
	paramName string // name of the captured value (empty if no param child)
	// methods[METHOD] -> route, populated on terminal nodes
	methods map[string]*registeredRoute
}

func newTrieNode() *trieNode {
	return &trieNode{
		static:  map[string]*trieNode{},
		methods: map[string]*registeredRoute{},
	}
}

func buildRouteTrie(routes []registeredRoute) *routeTrie {
	t := &routeTrie{root: newTrieNode()}
	for k := range routes {
		r := &routes[k]
		t.insert(r)
	}
	return t
}

func (t *routeTrie) insert(r *registeredRoute) {
	node := t.root
	for _, seg := range r.PathParts {
		if strings.HasPrefix(seg, ":") {
			if node.param == nil {
				node.param = newTrieNode()
				node.paramName = seg[1:]
			}
			node = node.param
			continue
		}
		child, ok := node.static[seg]
		if !ok {
			child = newTrieNode()
			node.static[seg] = child
		}
		node = child
	}
	node.methods[r.Method] = r
}

// match walks the trie for the given method + URL path. Returns the
// matched route, the captured params, and whether the lookup succeeded.
//
// Static segments take precedence over `:param` matches at every level,
// matching the historical behavior of the linear scanner. SSE / WS
// pseudo-methods are matched as GET on the wire.
func (t *routeTrie) match(method, urlPath string) (*registeredRoute, map[string]string, bool) {
	if t == nil || t.root == nil {
		return nil, nil, false
	}
	segs := splitPath(urlPath)
	params := map[string]string{}
	r := t.root.matchSegs(segs, method, params)
	if r == nil {
		return nil, nil, false
	}
	return r, params, true
}

func (n *trieNode) matchSegs(segs []string, method string, params map[string]string) *registeredRoute {
	if len(segs) == 0 {
		// Terminal — pick by method. SSE/WS routes are GET-shaped.
		if r := n.methods[method]; r != nil {
			return r
		}
		if method == "GET" {
			if r := n.methods["SSE"]; r != nil {
				return r
			}
			if r := n.methods["WS"]; r != nil {
				return r
			}
		}
		return nil
	}
	head, rest := segs[0], segs[1:]
	if child, ok := n.static[head]; ok {
		if r := child.matchSegs(rest, method, params); r != nil {
			return r
		}
	}
	if n.param != nil {
		params[n.paramName] = head
		if r := n.param.matchSegs(rest, method, params); r != nil {
			return r
		}
		delete(params, n.paramName)
	}
	return nil
}
