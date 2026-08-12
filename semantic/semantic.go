package semantic

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ben-ranford/stave/internal/canonical"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type NodeID string
type Role string
type State string
type RelationKind string
type ActionID string
type LiveMode string
type Stability string

const NodeIDNormalizationPolicy = "stave-node-id-v1:utf8-identity"

const (
	LiveNone          LiveMode  = "none"
	LivePolite        LiveMode  = "polite"
	LiveAssertive     LiveMode  = "assertive"
	Stable            Stability = "stable"
	SnapshotStability Stability = "snapshot"
)

var roles = map[Role]bool{"application": true, "window": true, "region": true, "group": true, "heading": true, "text": true, "status": true, "alert": true, "separator": true, "list": true, "listitem": true, "table": true, "rowgroup": true, "row": true, "columnheader": true, "rowheader": true, "cell": true, "button": true, "link": true, "checkbox": true, "radio": true, "radiogroup": true, "textbox": true, "searchbox": true, "combobox": true, "option": true, "form": true, "progressbar": true, "dialog": true, "tooltip": true, "menu": true, "menuitem": true, "tablist": true, "tab": true, "tabpanel": true, "document": true, "code": true, "img": true}
var interactive = map[Role]bool{"button": true, "link": true, "checkbox": true, "radio": true, "textbox": true, "searchbox": true, "combobox": true, "option": true, "menuitem": true, "tab": true}

type NodeKey struct {
	AppNamespace string `json:"appNamespace"`
	View         string `json:"view"`
	Kind         string `json:"kind"`
	Entity       string `json:"entity"`
	Slot         string `json:"slot"`
}

// Node identity uses stave-node-id-v1:utf8-identity: each UTF-8 field is encoded
// as its decimal byte length, a colon, then its bytes, concatenated in
// AppNamespace, View, Kind, Entity, Slot order. No Unicode normalization occurs.

func (k NodeKey) valid() bool {
	return k.AppNamespace != "" && k.View != "" && k.Kind != "" && k.Entity != "" && k.Slot != ""
}
func NodeIDFor(k NodeKey) (NodeID, error) {
	if !k.valid() {
		return "", errors.New("invalid node key")
	}
	vals := []string{k.AppNamespace, k.View, k.Kind, k.Entity, k.Slot}
	var b strings.Builder
	for _, v := range vals {
		if !utf8.ValidString(v) || strings.TrimSpace(v) == "" {
			return "", errors.New("invalid UTF-8 or empty node key field")
		}
		fmt.Fprintf(&b, "%d:", len(v))
		b.WriteString(v)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return NodeID("n1_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:16]))), nil
}
func (id NodeID) Valid() bool {
	if !strings.HasPrefix(string(id), "n1_") {
		return false
	}
	s := strings.TrimPrefix(string(id), "n1_")
	if s != strings.ToLower(s) {
		return false
	}
	b, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(s))
	return e == nil && len(b) == 16
}

func (n Node) MarshalJSON() ([]byte, error) {
	type dto struct {
		ID          NodeID            `json:"id"`
		Generation  uint32            `json:"generation"`
		Role        Role              `json:"role"`
		Name        string            `json:"name"`
		Description string            `json:"description,omitempty"`
		Value       Value             `json:"value,omitempty"`
		States      []State           `json:"states,omitempty"`
		Relations   []Relation        `json:"relations,omitempty"`
		Actions     []ActionRef       `json:"actions,omitempty"`
		Layout      LayoutSpec        `json:"layout,omitempty"`
		Style       StyleIntent       `json:"style,omitempty"`
		Children    []Node            `json:"children,omitempty"`
		Flags       Flags             `json:"flags"`
		Metadata    map[string]string `json:"metadata,omitempty"`
	}
	v := n.value
	if v.Redacted {
		v.Text = ""
	}
	return json.Marshal(dto{n.id, n.generation, n.role, n.name, n.description, v, n.states, n.relations, n.actions, n.layout, n.style, n.children, n.flags, n.metadata})
}
func (id NodeID) String() string { return string(id) }

type Value struct {
	Text     string `json:"text,omitempty"`
	Redacted bool   `json:"redacted,omitempty"`
	HasValue bool   `json:"hasValue,omitempty"`
}

func SecretValue() Value { return Value{Redacted: true, HasValue: true} }

type Relation struct {
	Kind   RelationKind `json:"kind"`
	Target NodeID       `json:"target"`
}
type ActionRef struct {
	ID      ActionID `json:"id"`
	Label   string   `json:"label,omitempty"`
	Default bool     `json:"default,omitempty"`
}
type Flags struct {
	Visible   bool      `json:"visible"`
	Focusable bool      `json:"focusable"`
	Disabled  bool      `json:"disabled"`
	Sensitive bool      `json:"sensitive"`
	Offscreen bool      `json:"offscreen"`
	Live      LiveMode  `json:"live,omitempty"`
	Stability Stability `json:"stability,omitempty"`
}
type LayoutSpec struct {
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}
type StyleIntent struct {
	Role string `json:"role,omitempty"`
}
type NodeSpec struct {
	ID                NodeID
	Key               *NodeKey
	Generation        uint32
	Role              Role
	Name, Description string
	Value             Value
	States            []State
	Relations         []Relation
	Actions           []ActionRef
	Layout            LayoutSpec
	Style             StyleIntent
	Children          []Node
	Flags             Flags
	Metadata          map[string]string
}
type Node struct {
	id                NodeID
	generation        uint32
	role              Role
	name, description string
	value             Value
	states            []State
	relations         []Relation
	actions           []ActionRef
	layout            LayoutSpec
	style             StyleIntent
	children          []Node
	flags             Flags
	metadata          map[string]string
}

func NewNode(s NodeSpec) (Node, error) {
	id := s.ID
	var err error
	if s.Key != nil {
		id, err = NodeIDFor(*s.Key)
		if err != nil {
			return Node{}, err
		}
	}
	if !id.Valid() {
		return Node{}, errors.New("invalid node id")
	}
	if !roles[s.Role] {
		return Node{}, fmt.Errorf("invalid role %q", s.Role)
	}
	for _, x := range []string{s.Name, s.Description, s.Value.Text} {
		if !utf8.ValidString(x) || containsUnsafeControl(x) {
			return Node{}, errors.New("invalid control or UTF-8 text")
		}
	}
	if interactive[s.Role] && strings.TrimSpace(s.Name) == "" {
		return Node{}, errors.New("interactive node requires accessible name")
	}
	if (s.Flags.Sensitive || s.Value.Redacted) && s.Value.Text != "" {
		return Node{}, errors.New("sensitive value must be redacted")
	}
	n := Node{id: id, generation: s.Generation, role: s.Role, name: s.Name, description: s.Description, value: s.Value, layout: s.Layout, style: s.Style, flags: s.Flags}
	n.states = append([]State(nil), s.States...)
	n.relations = append([]Relation(nil), s.Relations...)
	n.actions = append([]ActionRef(nil), s.Actions...)
	n.children = append([]Node(nil), s.Children...)
	n.metadata = map[string]string{}
	for k, v := range s.Metadata {
		n.metadata[k] = v
	}
	return n, nil
}

func containsUnsafeControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return true
		}
	}
	return false
}
func (n Node) ID() NodeID            { return n.id }
func (n Node) Generation() uint32    { return n.generation }
func (n Node) Role() Role            { return n.role }
func (n Node) Name() string          { return n.name }
func (n Node) Description() string   { return n.description }
func (n Node) Value() Value          { return n.value }
func (n Node) States() []State       { return append([]State(nil), n.states...) }
func (n Node) Relations() []Relation { return append([]Relation(nil), n.relations...) }
func (n Node) Actions() []ActionRef  { return append([]ActionRef(nil), n.actions...) }
func (n Node) Children() []Node      { return append([]Node(nil), n.children...) }
func (n Node) Flags() Flags          { return n.flags }
func (n Node) Layout() LayoutSpec    { return n.layout }
func (n Node) Style() StyleIntent    { return n.style }
func (n Node) ChildCount() int       { return len(n.children) }
func (n Node) MetadataLen() int      { return len(n.metadata) }
func (n Node) Child(index int) (Node, bool) {
	if index < 0 || index >= len(n.children) {
		return Node{}, false
	}
	return n.children[index], true
}
func (n Node) RangeMetadata(yield func(key, value string) bool) {
	for key, value := range n.metadata {
		if !yield(key, value) {
			return
		}
	}
}
func (n Node) Metadata() map[string]string {
	m := map[string]string{}
	for k, v := range n.metadata {
		m[k] = v
	}
	return m
}
func (n Node) WithChildren(c ...Node) (Node, error) {
	n.children = append([]Node(nil), c...)
	return n, nil
}

type Target struct {
	NodeID           NodeID `json:"nodeId"`
	Generation       uint32 `json:"generation"`
	ObservedRevision uint64 `json:"observedRevision"`
}
type Tree struct {
	schemaVersion string
	revision      uint64
	root          Node
	hash          [32]byte
}

type Snapshot struct {
	SchemaVersion string `json:"schemaVersion"`
	Revision      uint64 `json:"revision"`
	TreeHash      string `json:"treeHash"`
	Root          Node   `json:"root"`
}

func (t Tree) Snapshot() Snapshot { return Snapshot{t.schemaVersion, t.revision, t.Hash(), t.root} }

// Validate fails closed when a protocol or replay boundary receives a
// structurally invalid snapshot or metadata inconsistent with its tree.
func (s Snapshot) Validate() error {
	if s.SchemaVersion != "stave-semantic-v1" || s.Revision == 0 || s.TreeHash == "" {
		return errors.New("invalid semantic snapshot metadata")
	}
	t, err := NewTree(s.Revision, s.Root)
	if err != nil {
		return err
	}
	if t.Hash() != s.TreeHash {
		return errors.New("semantic snapshot hash mismatch")
	}
	return nil
}

type Patch struct {
	FromRevision      uint64   `json:"fromRevision"`
	ToRevision        uint64   `json:"toRevision"`
	Added             []NodeID `json:"added,omitempty"`
	Removed           []NodeID `json:"removed,omitempty"`
	GenerationChanged []NodeID `json:"generationChanged,omitempty"`
}

// Validate checks patch revision ordering, stable NodeID syntax, and duplicate
// membership across patch sets.
func (p Patch) Validate() error {
	if p.FromRevision == 0 || p.ToRevision <= p.FromRevision {
		return errors.New("invalid semantic patch revisions")
	}
	seen := make(map[NodeID]string, len(p.Added)+len(p.Removed)+len(p.GenerationChanged))
	for kind, ids := range map[string][]NodeID{"added": p.Added, "removed": p.Removed, "generationChanged": p.GenerationChanged} {
		for _, id := range ids {
			if !id.Valid() {
				return errors.New("invalid semantic patch node id")
			}
			if previous, ok := seen[id]; ok {
				return fmt.Errorf("semantic patch node %s appears in %s and %s", id, previous, kind)
			}
			seen[id] = kind
		}
	}
	return nil
}

func Diff(a, b Tree) Patch {
	p := Patch{FromRevision: a.revision, ToRevision: b.revision}
	am, bm := map[NodeID]uint32{}, map[NodeID]uint32{}
	var collect func(Node, map[NodeID]uint32)
	collect = func(n Node, m map[NodeID]uint32) {
		m[n.id] = n.generation
		for _, c := range n.children {
			collect(c, m)
		}
	}
	collect(a.root, am)
	collect(b.root, bm)
	for id := range bm {
		if _, ok := am[id]; !ok {
			p.Added = append(p.Added, id)
		} else if am[id] != bm[id] {
			p.GenerationChanged = append(p.GenerationChanged, id)
		}
	}
	for id := range am {
		if _, ok := bm[id]; !ok {
			p.Removed = append(p.Removed, id)
		}
	}
	sort.Slice(p.Added, func(i, j int) bool { return p.Added[i] < p.Added[j] })
	sort.Slice(p.Removed, func(i, j int) bool { return p.Removed[i] < p.Removed[j] })
	sort.Slice(p.GenerationChanged, func(i, j int) bool { return p.GenerationChanged[i] < p.GenerationChanged[j] })
	return p
}

type Tombstone struct {
	ID              NodeID `json:"id"`
	Generation      uint32 `json:"generation"`
	RemovedRevision uint64 `json:"removedRevision"`
}
type Tombstones struct{ m map[NodeID]Tombstone }

func NewTombstones() *Tombstones { return &Tombstones{m: map[NodeID]Tombstone{}} }
func (t *Tombstones) Remove(id NodeID, g uint32, r uint64) {
	if t == nil {
		return
	}
	if old, ok := t.m[id]; !ok || g >= old.Generation {
		t.m[id] = Tombstone{id, g, r}
	}
}
func (t *Tombstones) Get(id NodeID) (Tombstone, bool) {
	if t == nil {
		return Tombstone{}, false
	}
	x, ok := t.m[id]
	return x, ok
}
func (t *Tombstones) RestoreGeneration(id NodeID) uint32 {
	if x, ok := t.Get(id); ok {
		return x.Generation + 1
	}
	return 0
}

func NewTree(revision uint64, root Node) (Tree, error) {
	t := Tree{schemaVersion: "stave-semantic-v1", revision: revision, root: root}
	if err := t.Validate(); err != nil {
		return Tree{}, err
	}
	var err error
	t.hash, err = canonical.Hash(t.snapshot())
	if err != nil {
		return Tree{}, err
	}
	return t, nil
}
func (t Tree) Validate() error {
	seen := map[NodeID]uint32{}
	var walk func(Node, int) error
	walk = func(n Node, d int) error {
		if d > 1024 {
			return errors.New("resource limit: depth")
		}
		if !n.id.Valid() {
			return errors.New("invalid node id")
		}
		if g, ok := seen[n.id]; ok {
			return fmt.Errorf("duplicate node id %s generations %d/%d", n.id, g, n.generation)
		}
		seen[n.id] = n.generation
		for _, r := range n.relations {
			if !r.Target.Valid() {
				return errors.New("invalid relation target")
			}
		}
		for _, c := range n.children {
			if err := walk(c, d+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(t.root, 0); err != nil {
		return err
	}
	var check func(Node) error
	check = func(n Node) error {
		for _, r := range n.relations {
			if !containsNode(t.root, r.Target) {
				return fmt.Errorf("dangling relation %s", r.Target)
			}
		}
		for _, c := range n.children {
			if err := check(c); err != nil {
				return err
			}
		}
		return nil
	}
	return check(t.root)
}
func containsNode(n Node, id NodeID) bool {
	if n.id == id {
		return true
	}
	for _, c := range n.children {
		if containsNode(c, id) {
			return true
		}
	}
	return false
}
func (t Tree) Root() Node            { return t.root }
func (t Tree) Revision() uint64      { return t.revision }
func (t Tree) Hash() string          { return hex.EncodeToString(t.hash[:]) }
func (t Tree) SchemaVersion() string { return t.schemaVersion }

// WithRevision rebuilds the immutable tree under a session-owned revision.
// Applications define semantics; the session owns revision sequencing.
func (t Tree) WithRevision(revision uint64) (Tree, error) {
	return NewTree(revision, t.root)
}

type snapshotNode struct {
	ID          NodeID            `json:"id"`
	Generation  uint32            `json:"generation"`
	Role        Role              `json:"role"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Value       Value             `json:"value,omitempty"`
	States      []State           `json:"states,omitempty"`
	Relations   []Relation        `json:"relations,omitempty"`
	Actions     []ActionRef       `json:"actions,omitempty"`
	Layout      LayoutSpec        `json:"layout,omitempty"`
	Style       StyleIntent       `json:"style,omitempty"`
	Children    []snapshotNode    `json:"children,omitempty"`
	Flags       Flags             `json:"flags"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

func (n Node) snap() snapshotNode {
	v := n.value
	if v.Redacted {
		v.Text = ""
	}
	x := snapshotNode{n.id, n.generation, n.role, n.name, n.description, v, n.states, n.relations, n.actions, n.layout, n.style, nil, n.flags, n.metadata}
	for _, c := range n.children {
		x.Children = append(x.Children, c.snap())
	}
	return x
}
func (t Tree) snapshot() any {
	return struct {
		SchemaVersion string       `json:"schemaVersion"`
		Revision      uint64       `json:"revision"`
		Root          snapshotNode `json:"root"`
	}{t.schemaVersion, t.revision, t.root.snap()}
}
func (t Tree) MarshalJSON() ([]byte, error) { return json.Marshal(t.snapshot()) }
