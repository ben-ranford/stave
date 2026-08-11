package stave

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

var (
	ErrInvalidTree      = errors.New("stave: invalid tree")
	ErrStaleRevision    = errors.New("stave: stale revision")
	ErrUnknownAction    = errors.New("stave: unknown action")
	ErrInvalidArguments = errors.New("stave: invalid action arguments")
	ErrInvalidAction    = errors.New("stave: invalid action")
	ErrTargetNotFound   = errors.New("stave: target not found")
	ErrTargetRequired   = errors.New("stave: target required")
	ErrUnexpectedTarget = errors.New("stave: unexpected target")
	ErrAmbiguousAction  = errors.New("stave: ambiguous action")
)

const (
	// ProvisionalIDAlgorithm deliberately does not claim compatibility with the
	// complete stave-node-id-v1 contract in the system design.
	ProvisionalIDAlgorithm = "stave-node-id-spike-v0"
	MaxTreeDepth           = 256
	MaxTreeNodes           = 100_000
	MaxNodeTextBytes       = 1 << 20
	MaxTreeTextBytes       = 16 << 20
	MaxSnapshotBytes       = 32 << 20
)

type SemanticRole string

const (
	RoleScreen       SemanticRole = "screen"
	RoleMasterDetail SemanticRole = "master_detail"
	RoleRecord       SemanticRole = "record"
	RolePanel        SemanticRole = "panel"
	RoleDashboard    SemanticRole = "dashboard"
)

type StyleIntent string

const (
	IntentPrimary StyleIntent = "primary"
	IntentMuted   StyleIntent = "muted"
	IntentAccent  StyleIntent = "accent"
	IntentError   StyleIntent = "error"
)

type Node struct {
	id, namespace, key string
	generation         uint64
	label, content     string
	role               SemanticRole
	style              StyleIntent
	actions            []string
	children           []Node
}

func NewNode(namespace, key string, generation uint64, role SemanticRole, label, content string, children ...Node) (Node, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(key) == "" || generation == 0 || strings.TrimSpace(string(role)) == "" {
		return Node{}, fmt.Errorf("%w: namespace, key, generation and semantic role are required", ErrInvalidTree)
	}
	if len(namespace)+len(key)+len(label)+len(content) > MaxNodeTextBytes {
		return Node{}, fmt.Errorf("%w: node text exceeds %d bytes", ErrInvalidTree, MaxNodeTextBytes)
	}
	c := append([]Node(nil), children...)
	return Node{id: ProvisionalID(namespace, key), namespace: namespace, key: key, generation: generation, role: role, label: label, content: content, children: c}, nil
}
func ProvisionalID(namespace, key string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(ProvisionalIDAlgorithm))
	_ = binary.Write(h, binary.BigEndian, uint64(len(namespace)))
	_, _ = h.Write([]byte(namespace))
	_ = binary.Write(h, binary.BigEndian, uint64(len(key)))
	_, _ = h.Write([]byte(key))
	return "s0_" + hex.EncodeToString(h.Sum(nil)[:16])
}
func (n Node) ID() string                         { return n.id }
func (n Node) Namespace() string                  { return n.namespace }
func (n Node) Key() string                        { return n.key }
func (n Node) Generation() uint64                 { return n.generation }
func (n Node) Role() SemanticRole                 { return n.role }
func (n Node) Label() string                      { return n.label }
func (n Node) Content() string                    { return n.content }
func (n Node) StyleIntent() StyleIntent           { return n.style }
func (n Node) Children() []Node                   { return append([]Node(nil), n.children...) }
func (n Node) Actions() []string                  { return append([]string(nil), n.actions...) }
func (n Node) WithStyleIntent(v StyleIntent) Node { n.style = v; return n }
func (n Node) WithContent(v string) Node          { n.content = v; return n }
func (n Node) WithActions(names ...string) Node {
	n.actions = append([]string(nil), names...)
	return n
}

type Tree struct {
	Root     Node
	Revision uint64
}

func (t Tree) Validate() error {
	if t.Revision == 0 {
		return fmt.Errorf("%w: revision must be positive", ErrInvalidTree)
	}
	seen := map[string]bool{}
	nodeCount := 0
	textBytes := 0
	var walk func(Node, int) error
	walk = func(n Node, depth int) error {
		if depth > MaxTreeDepth {
			return fmt.Errorf("%w: depth exceeds %d", ErrInvalidTree, MaxTreeDepth)
		}
		nodeCount++
		if nodeCount > MaxTreeNodes {
			return fmt.Errorf("%w: node count exceeds %d", ErrInvalidTree, MaxTreeNodes)
		}
		if n.generation == 0 || strings.TrimSpace(n.namespace) == "" || strings.TrimSpace(n.key) == "" || strings.TrimSpace(string(n.role)) == "" {
			return fmt.Errorf("%w: malformed node", ErrInvalidTree)
		}
		textBytes += len(n.namespace) + len(n.key) + len(n.label) + len(n.content)
		if textBytes > MaxTreeTextBytes {
			return fmt.Errorf("%w: tree text exceeds %d bytes", ErrInvalidTree, MaxTreeTextBytes)
		}
		actionNames := map[string]bool{}
		for _, action := range n.actions {
			if strings.TrimSpace(action) == "" || actionNames[action] {
				return fmt.Errorf("%w: blank or duplicate node action reference", ErrInvalidTree)
			}
			actionNames[action] = true
			textBytes += len(action)
		}
		if n.id != ProvisionalID(n.namespace, n.key) {
			return fmt.Errorf("%w: unstable node id", ErrInvalidTree)
		}
		if seen[n.id] {
			return fmt.Errorf("%w: duplicate node id", ErrInvalidTree)
		}
		seen[n.id] = true
		for _, c := range n.children {
			if err := walk(c, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(t.Root, 1)
}

func (t Tree) Find(id string) (Node, bool) {
	stack := []Node{t.Root}
	for visited := 0; len(stack) > 0 && visited < MaxTreeNodes; visited++ {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		if node.id == id {
			return node, true
		}
		remaining := MaxTreeNodes - visited - 1
		if len(node.children) > remaining {
			return Node{}, false
		}
		stack = append(stack, node.children...)
	}
	return Node{}, false
}

func (t Tree) HasAction(name string) bool {
	return t.ActionRefCount(name) > 0
}

func (t Tree) ActionRefCount(name string) int {
	count := 0
	stack := []Node{t.Root}
	for visited := 0; len(stack) > 0 && visited < MaxTreeNodes; visited++ {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		for _, action := range node.actions {
			if action == name {
				count++
			}
		}
		remaining := MaxTreeNodes - visited - 1
		if len(node.children) > remaining {
			return 0
		}
		stack = append(stack, node.children...)
	}
	return count
}

type Field struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type ActionScope string

const (
	ScopeGlobal ActionScope = "global"
	ScopeTarget ActionScope = "target_required"
)

type Action struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Scope       ActionScope            `json:"scope"`
	Fields      []Field                `json:"fields,omitempty"`
	Handler     func(ActionCall) error `json:"-"`
}

type ActionCall struct {
	Source    InvocationSource
	Revision  uint64
	Target    *TargetRef
	Arguments map[string]any
}

func (a Action) ValidateArgs(args map[string]any) error {
	known := map[string]bool{}
	for _, f := range a.Fields {
		known[f.Name] = true
		if f.Type != "string" && f.Type != "number" && f.Type != "boolean" {
			return fmt.Errorf("%w: unsupported field type %s", ErrInvalidArguments, f.Type)
		}
		v, ok := args[f.Name]
		if f.Required && !ok {
			return fmt.Errorf("%w: missing %s", ErrInvalidArguments, f.Name)
		}
		if !ok {
			continue
		}
		switch f.Type {
		case "string":
			if _, ok := v.(string); !ok {
				return fmt.Errorf("%w: %s must be string", ErrInvalidArguments, f.Name)
			}
		case "number":
			if _, ok := v.(float64); !ok {
				return fmt.Errorf("%w: %s must be number", ErrInvalidArguments, f.Name)
			}
		case "boolean":
			if _, ok := v.(bool); !ok {
				return fmt.Errorf("%w: %s must be boolean", ErrInvalidArguments, f.Name)
			}
		}
		if len(f.Enum) > 0 {
			s, ok := v.(string)
			valid := false
			for _, e := range f.Enum {
				if ok && s == e {
					valid = true
				}
			}
			if !valid {
				return fmt.Errorf("%w: %s outside enum", ErrInvalidArguments, f.Name)
			}
		}
	}
	unknown := make([]string, 0)
	for name := range args {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("%w: unknown fields %s", ErrInvalidArguments, strings.Join(unknown, ", "))
	}
	return nil
}

type ActionRegistry struct{ actions map[string]Action }

func NewActionRegistry(actions ...Action) (ActionRegistry, error) {
	m := map[string]Action{}
	for _, a := range actions {
		if strings.TrimSpace(a.Name) == "" {
			return ActionRegistry{}, fmt.Errorf("%w: blank name", ErrInvalidAction)
		}
		if _, exists := m[a.Name]; exists {
			return ActionRegistry{}, fmt.Errorf("%w: duplicate name %s", ErrInvalidAction, a.Name)
		}
		if a.Scope != ScopeGlobal && a.Scope != ScopeTarget {
			return ActionRegistry{}, fmt.Errorf("%w: action %s has invalid scope", ErrInvalidAction, a.Name)
		}
		fieldNames := map[string]bool{}
		for _, field := range a.Fields {
			if strings.TrimSpace(field.Name) == "" || fieldNames[field.Name] {
				return ActionRegistry{}, fmt.Errorf("%w: invalid or duplicate field in %s", ErrInvalidAction, a.Name)
			}
			if field.Type != "string" && field.Type != "number" && field.Type != "boolean" {
				return ActionRegistry{}, fmt.Errorf("%w: unsupported field type %s", ErrInvalidAction, field.Type)
			}
			fieldNames[field.Name] = true
		}
		m[a.Name] = cloneAction(a)
	}
	return ActionRegistry{m}, nil
}
func (r ActionRegistry) invoke(name string, call ActionCall) error {
	a, ok := r.actions[name]
	if !ok {
		return ErrUnknownAction
	}
	if err := a.ValidateArgs(call.Arguments); err != nil {
		return err
	}
	if a.Handler == nil {
		return nil
	}
	return a.Handler(call)
}
func (r ActionRegistry) Manifest() []Action {
	out := make([]Action, 0, len(r.actions))
	for _, a := range r.actions {
		manifestAction := cloneAction(a)
		manifestAction.Handler = nil
		out = append(out, manifestAction)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cloneAction(action Action) Action {
	cloned := action
	cloned.Fields = make([]Field, len(action.Fields))
	for i, field := range action.Fields {
		cloned.Fields[i] = field
		cloned.Fields[i].Enum = append([]string(nil), field.Enum...)
	}
	return cloned
}

func (r ActionRegistry) action(name string) (Action, bool) {
	action, ok := r.actions[name]
	return action, ok
}

type Capabilities struct {
	Width         int  `json:"width"`
	Color         bool `json:"color"`
	Interactive   bool `json:"interactive"`
	ReducedMotion bool `json:"reduced_motion"`
	ASCII         bool `json:"ascii"`
}

func (c Capabilities) Degrade() Capabilities {
	if c.Width < 40 {
		c.ASCII = true
	}
	return c
}

type Theme struct {
	name   string
	tokens map[StyleIntent]string
}

func NewTheme(name string, tokens map[StyleIntent]string) (Theme, error) {
	theme := Theme{name: name, tokens: make(map[StyleIntent]string, len(tokens))}
	for intent, token := range tokens {
		theme.tokens[intent] = token
	}
	if err := theme.Validate(); err != nil {
		return Theme{}, err
	}
	return theme, nil
}

func (t Theme) Name() string { return t.name }

func (t Theme) Token(intent StyleIntent) string { return t.tokens[intent] }

func (t Theme) Validate() error {
	if strings.TrimSpace(t.name) == "" {
		return errors.New("stave: theme name is required")
	}
	for _, intent := range []StyleIntent{IntentPrimary, IntentMuted, IntentAccent, IntentError} {
		token := t.tokens[intent]
		if strings.TrimSpace(token) == "" {
			return fmt.Errorf("theme %s missing %s", t.name, intent)
		}
		if containsControl(token) {
			return fmt.Errorf("theme %s contains control characters in %s", t.name, intent)
		}
	}
	return nil
}

type Renderer struct {
	Caps  Capabilities
	Theme Theme
}

func (r Renderer) Snapshot(t Tree) ([]byte, error) {
	if err := r.Theme.Validate(); err != nil {
		return nil, err
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	type snap struct {
		ID          string       `json:"id"`
		Generation  uint64       `json:"generation"`
		Role        SemanticRole `json:"role"`
		StyleIntent StyleIntent  `json:"style_intent,omitempty"`
		Label       string       `json:"label"`
		Content     string       `json:"content"`
		Actions     []string     `json:"actions,omitempty"`
		Children    []snap       `json:"children,omitempty"`
	}
	var conv func(Node) snap
	conv = func(n Node) snap {
		s := snap{
			ID:          n.id,
			Generation:  n.generation,
			Role:        n.role,
			StyleIntent: n.style,
			Label:       n.label,
			Content:     n.content,
			Actions:     append([]string(nil), n.actions...),
		}
		for _, c := range n.children {
			s.Children = append(s.Children, conv(c))
		}
		return s
	}
	encoded, err := json.Marshal(struct {
		Revision uint64 `json:"revision"`
		Root     snap   `json:"root"`
	}{t.Revision, conv(t.Root)})
	if err != nil {
		return nil, err
	}
	if len(encoded) > MaxSnapshotBytes {
		return nil, fmt.Errorf("stave: snapshot exceeds %d bytes", MaxSnapshotBytes)
	}
	return encoded, nil
}
func (r Renderer) RenderPlain(t Tree) (string, error) {
	if _, err := r.Snapshot(t); err != nil {
		return "", err
	}
	var b strings.Builder
	var walk func(Node, int)
	walk = func(n Node, d int) {
		indent := strings.Repeat("  ", d)
		label := sanitizeText(n.label)
		if label == "" {
			label = sanitizeText(string(n.role))
		}
		prefix := r.Theme.Token(n.style)
		if prefix == "" {
			prefix = r.Theme.Token(IntentPrimary)
		}
		if !r.Caps.Color {
			prefix = ""
		}
		if r.Caps.ASCII || r.Caps.Width < 40 {
			b.WriteString(indent + prefix + label)
			if n.content != "" {
				b.WriteString(": " + sanitizeText(firstLine(n.content)))
			}
			b.WriteByte('\n')
		} else {
			b.WriteString(indent + prefix + "[" + label + "]")
			if n.content != "" {
				b.WriteString(" " + sanitizeText(firstLine(n.content)))
			}
			b.WriteByte('\n')
		}
		for _, c := range n.children {
			walk(c, d+1)
		}
	}
	walk(t.Root, 0)
	if b.Len() > MaxSnapshotBytes {
		return "", fmt.Errorf("stave: rendered output exceeds %d bytes", MaxSnapshotBytes)
	}
	return b.String(), nil
}
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func containsControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func sanitizeText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '\uFFFD'
		}
		return r
	}, s)
}

type Protocol struct {
	Tree     Tree
	Actions  ActionRegistry
	Renderer Renderer
}

func (p Protocol) Snapshot() ([]byte, error) {
	if err := p.validateContract(); err != nil {
		return nil, err
	}
	return p.Renderer.Snapshot(p.Tree)
}

type InvocationSource string

const (
	SourceHuman InvocationSource = "human"
	SourceAgent InvocationSource = "agent"
)

type TargetRef struct {
	NodeID           string `json:"node_id"`
	Generation       uint64 `json:"generation"`
	ObservedRevision uint64 `json:"observed_revision"`
}

type Invocation struct {
	Source           InvocationSource `json:"source"`
	Action           string           `json:"action"`
	ObservedRevision uint64           `json:"observed_revision"`
	Target           *TargetRef       `json:"target,omitempty"`
	Arguments        map[string]any   `json:"arguments,omitempty"`
}

func (p Protocol) Dispatch(invocation Invocation) error {
	if invocation.Source != SourceHuman && invocation.Source != SourceAgent {
		return fmt.Errorf("%w: invalid invocation source", ErrInvalidAction)
	}
	if err := p.validateContract(); err != nil {
		return err
	}
	target := cloneTarget(invocation.Target)
	arguments := cloneArguments(invocation.Arguments)
	action, ok := p.Actions.action(invocation.Action)
	if !ok {
		return ErrUnknownAction
	}
	if action.Scope == ScopeTarget && target == nil {
		return ErrTargetRequired
	}
	if action.Scope == ScopeGlobal && target != nil {
		return ErrUnexpectedTarget
	}
	observedRevision := invocation.ObservedRevision
	if target != nil {
		if invocation.ObservedRevision != 0 && invocation.ObservedRevision != target.ObservedRevision {
			return ErrStaleRevision
		}
		observedRevision = target.ObservedRevision
	}
	if observedRevision != p.Tree.Revision {
		return ErrStaleRevision
	}
	if target != nil {
		node, ok := p.Tree.Find(target.NodeID)
		if !ok {
			return ErrTargetNotFound
		}
		if node.Generation() != target.Generation {
			return ErrStaleRevision
		}
		allowed := false
		for _, action := range node.Actions() {
			if action == invocation.Action {
				allowed = true
				break
			}
		}
		if !allowed {
			return ErrUnknownAction
		}
	} else if p.Tree.ActionRefCount(invocation.Action) != 1 {
		return ErrAmbiguousAction
	}
	call := ActionCall{
		Source:    invocation.Source,
		Revision:  p.Tree.Revision,
		Target:    target,
		Arguments: arguments,
	}
	return p.Actions.invoke(invocation.Action, call)
}

// Manifest is the agent-facing, deterministic description of capabilities and actions.
func (p Protocol) Manifest() ([]byte, error) {
	if err := p.validateContract(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Capabilities Capabilities `json:"capabilities"`
		Actions      []Action     `json:"actions"`
	}{p.Renderer.Caps.Degrade(), p.Actions.Manifest()})
}

func (p Protocol) validateContract() error {
	if err := p.Tree.Validate(); err != nil {
		return err
	}
	counts := make(map[string]int)
	stack := []Node{p.Tree.Root}
	for len(stack) > 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		for _, name := range node.actions {
			if _, ok := p.Actions.action(name); !ok {
				return fmt.Errorf("%w: node references unregistered action %s", ErrInvalidTree, name)
			}
			counts[name]++
		}
		stack = append(stack, node.children...)
	}
	actionNames := make([]string, 0, len(p.Actions.actions))
	for name := range p.Actions.actions {
		actionNames = append(actionNames, name)
	}
	sort.Strings(actionNames)
	for _, name := range actionNames {
		action := p.Actions.actions[name]
		count := counts[name]
		if count == 0 {
			return fmt.Errorf("%w: registered action %s is not exposed by the tree", ErrInvalidTree, name)
		}
		if action.Scope == ScopeGlobal && count != 1 {
			return fmt.Errorf("%w: global action %s has %d references", ErrAmbiguousAction, name, count)
		}
	}
	return nil
}

func cloneTarget(target *TargetRef) *TargetRef {
	if target == nil {
		return nil
	}
	cloned := *target
	return &cloned
}

func cloneArguments(arguments map[string]any) map[string]any {
	if arguments == nil {
		return nil
	}
	cloned := make(map[string]any, len(arguments))
	for name, value := range arguments {
		cloned[name] = value
	}
	return cloned
}

type ReplayEvent struct {
	Sequence uint64 `json:"sequence"`
	Name     string `json:"name"`
	Payload  string `json:"payload,omitempty"`
}

type Reducer func(Tree, ReplayEvent) (Tree, error)

func Replay(initial Tree, events []ReplayEvent, reduce Reducer) (Tree, error) {
	if reduce == nil {
		return Tree{}, errors.New("stave: reducer is required")
	}
	if err := initial.Validate(); err != nil {
		return Tree{}, err
	}
	current := initial
	for index, event := range events {
		if event.Sequence != uint64(index+1) {
			return Tree{}, fmt.Errorf("stave: replay sequence gap at %d", index+1)
		}
		next, err := reduce(current, event)
		if err != nil {
			return Tree{}, err
		}
		if next.Revision != current.Revision+1 {
			return Tree{}, fmt.Errorf("stave: reducer revision must advance by one")
		}
		if err := next.Validate(); err != nil {
			return Tree{}, err
		}
		current = next
	}
	return current, nil
}
