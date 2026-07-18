package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Finding is one line of a config report: a key path (or offending value) and a
// short note saying what is wrong or what the key does.
type Finding struct {
	Path string
	Note string
}

// Report is how one config file differs from what the current binary
// understands. Dead and Values are problems; Stale and New are informational.
type Report struct {
	Missing  bool      // file does not exist; defaults apply
	ParseErr string    // file is not valid YAML
	Dead     []Finding // active keys the current schema does not know
	Stale    []Finding // commented keys the current schema does not know
	New      []Finding // template keys the file never mentions
	Values   []Finding // values the loader rejects or the code silently ignores
}

// CheckGlobal diffs a ~/.kovan/config.yaml against the current schema and the
// current commented template.
func CheckGlobal(data []byte) *Report {
	return check(data, schemaOf(Global{}), globalTemplate, func() any { return &Global{} })
}

// CheckRepo diffs a <repo>/.kovan.yaml against the current schema and template.
func CheckRepo(data []byte) *Report {
	return check(data, schemaOf(Repo{}), repoTemplate, func() any { return &Repo{} })
}

func check(data []byte, schema map[string]nodeKind, template string, newTarget func() any) *Report {
	rep := &Report{}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		rep.ParseErr = err.Error()
		return rep
	}
	active := activePaths(&doc, schema)
	commented := parseKeys(string(data), schema)
	activeSet := pathSet(active)
	mentioned := pathSet(append(commented, active...))

	rep.Dead = unknownFindings(active, schema, nil,
		"unknown key; the current binary ignores it")
	rep.Stale = unknownFindings(commented, schema, activeSet,
		"commented as a key that no longer exists")
	rep.New = newFindings(parseKeys(template, schema), mentioned)
	rep.Values = valueFindings(data, newTarget())
	return rep
}

// nodeKind classifies a schema path so parsers know how to treat its children.
type nodeKind int

const (
	kindLeaf   nodeKind = iota // scalar or list of scalars
	kindStruct                 // fixed set of child keys
	kindMap                    // map of structs; children live under a "*" segment
	kindList                   // list of structs; children live under a "[]" segment
)

// schemaOf walks a config struct's yaml tags and returns every known key path,
// e.g. "gates.push", "gates.patterns[].match", "accounts.*.token_file".
func schemaOf(v any) map[string]nodeKind {
	s := map[string]nodeKind{}
	walkStruct(reflect.TypeOf(v), "", s)
	return s
}

func walkStruct(t reflect.Type, prefix string, s map[string]nodeKind) {
	for i := 0; i < t.NumField(); i++ {
		tag := strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		path := joinPath(prefix, tag)
		ft := t.Field(i).Type
		switch {
		case ft.Kind() == reflect.Struct:
			s[path] = kindStruct
			walkStruct(ft, path, s)
		case ft.Kind() == reflect.Map && ft.Elem().Kind() == reflect.Struct:
			s[path] = kindMap
			s[path+".*"] = kindStruct
			walkStruct(ft.Elem(), path+".*", s)
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct:
			s[path] = kindList
			s[path+"[]"] = kindStruct
			walkStruct(ft.Elem(), path+"[]", s)
		default:
			s[path] = kindLeaf
		}
	}
}

func joinPath(prefix, seg string) string {
	if prefix == "" {
		return seg
	}
	return prefix + "." + seg
}

// parentOf steps one segment up a key path; "" is the root.
func parentOf(path string) string {
	if strings.HasSuffix(path, "[]") {
		return path[:len(path)-2]
	}
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[:i]
	}
	return ""
}

// mention is one key a file or template talks about: the schema-normalized path
// (map keys collapsed to "*"), the path as written, and any trailing comment.
type mention struct {
	path string
	raw  string
	doc  string
}

func pathSet(ms []mention) map[string]bool {
	set := map[string]bool{}
	for _, m := range ms {
		set[m.path] = true
	}
	return set
}

// keyLine matches an uncommented YAML key line: indentation, an optional list
// dash, and a snake_case key. Prose comments almost never look like this.
var keyLine = regexp.MustCompile(`^(\s*)(- )?([a-z0-9_]+):(.*)$`)

// parseKeys scans yaml-shaped text line by line, uncommenting as it goes, and
// returns every key path it mentions, active and commented alike, in order.
func parseKeys(text string, schema map[string]nodeKind) []mention {
	type frame struct {
		indent    int
		path, raw string
	}
	var stack []frame
	var out []mention
	pop := func(indent int) {
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
	}
	for _, line := range strings.Split(text, "\n") {
		m := keyLine.FindStringSubmatch(uncomment(line))
		if m == nil {
			continue
		}
		indent, key, rest := len(m[1]), m[3], m[4]
		if m[2] != "" { // a "- key:" list item: open an item frame under the list
			pop(indent)
			parent := frame{indent: -1}
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			stack = append(stack, frame{indent, parent.path + "[]", parent.raw + "[]"})
			indent += 2
		}
		pop(indent)
		parentPath, parentRaw := "", ""
		if len(stack) > 0 {
			parentPath, parentRaw = stack[len(stack)-1].path, stack[len(stack)-1].raw
		}
		seg := key
		if schema[parentPath] == kindMap {
			seg = "*"
		}
		path, raw := joinPath(parentPath, seg), joinPath(parentRaw, key)
		stack = append(stack, frame{indent, path, raw})
		out = append(out, mention{path: path, raw: raw, doc: trailingComment(rest)})
	}
	return out
}

// uncomment turns a whole-line comment back into the line it documents,
// preserving indentation; non-comment lines pass through untouched.
func uncomment(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return line
	}
	body := strings.TrimPrefix(strings.TrimPrefix(trimmed, "#"), " ")
	return line[:len(line)-len(trimmed)] + body
}

func trailingComment(rest string) string {
	if i := strings.Index(rest, " # "); i >= 0 {
		return strings.TrimSpace(rest[i+3:])
	}
	return ""
}

// activePaths walks parsed YAML and returns every key path actually set.
func activePaths(doc *yaml.Node, schema map[string]nodeKind) []mention {
	var out []mention
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return out
	}
	walkNode(doc.Content[0], "", "", schema, &out)
	return out
}

func walkNode(n *yaml.Node, path, raw string, schema map[string]nodeKind, out *[]mention) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			seg := key
			if schema[path] == kindMap {
				seg = "*"
			}
			p, r := joinPath(path, seg), joinPath(raw, key)
			*out = append(*out, mention{path: p, raw: r})
			walkNode(n.Content[i+1], p, r, schema, out)
		}
	case yaml.SequenceNode:
		for _, item := range n.Content {
			if item.Kind == yaml.MappingNode {
				walkNode(item, path+"[]", raw+"[]", schema, out)
			}
		}
	}
}

// unknownFindings reports mentions the schema does not know, collapsed to the
// highest unknown ancestor. skip drops mentions whose path is already covered
// elsewhere (a commented key that is also active).
func unknownFindings(ms []mention, schema map[string]nodeKind, skip map[string]bool, note string) []Finding {
	var out []Finding
	reported := map[string]bool{}
	seen := map[string]bool{}
	for _, m := range ms {
		if _, known := schema[m.path]; known || skip[m.path] || seen[m.raw] {
			continue
		}
		if ancestorIn(m.path, reported) {
			continue
		}
		reported[m.path] = true
		seen[m.raw] = true
		out = append(out, Finding{Path: m.raw, Note: note})
	}
	return out
}

// newFindings reports template keys the file never mentions, collapsed to the
// highest unmentioned ancestor. A doc-less container borrows the first
// descendant's doc line so every entry stays self-explaining.
func newFindings(tmpl []mention, mentioned map[string]bool) []Finding {
	var out []Finding
	reported := map[string]bool{}
	borrowed := map[string]int{} // path → index in out awaiting a doc
	for _, m := range tmpl {
		if mentioned[m.path] {
			continue
		}
		if anc := reportedAncestor(m.path, reported); anc != "" {
			if i, ok := borrowed[anc]; ok && m.doc != "" {
				out[i].Note = m.doc
				delete(borrowed, anc)
			}
			continue
		}
		reported[m.path] = true
		out = append(out, Finding{Path: m.raw, Note: m.doc})
		if m.doc == "" {
			borrowed[m.path] = len(out) - 1
		}
	}
	return out
}

func ancestorIn(path string, set map[string]bool) bool {
	return reportedAncestor(path, set) != ""
}

func reportedAncestor(path string, set map[string]bool) string {
	for p := parentOf(path); p != ""; p = parentOf(p) {
		if set[p] {
			return p
		}
	}
	return ""
}

// valueFindings strict-decodes the file into the real config struct and keeps
// the type errors. Unknown-field errors are dropped: the key diff already
// reports those with better wording.
func valueFindings(data []byte, target any) []Finding {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	err := dec.Decode(target)
	if err == nil || errors.Is(err, io.EOF) {
		return nil
	}
	var te *yaml.TypeError
	if !errors.As(err, &te) {
		return []Finding{{Path: fmt.Sprintf("cannot decode: %v", err)}}
	}
	var out []Finding
	for _, e := range te.Errors {
		if strings.Contains(e, "not found in type") {
			continue
		}
		out = append(out, Finding{Path: e})
	}
	return out
}
