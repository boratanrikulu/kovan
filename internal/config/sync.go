package config

import "strings"

// SyncGlobal rewrites a ~/.kovan/config.yaml against the current template:
// template documentation refreshes, user-set lines are preserved verbatim,
// and dead keys are dropped only when remove[rawPath] says so. Pure; the
// caller owns prompting, backups, and writing.
func SyncGlobal(data []byte, remove map[string]bool) []byte {
	return syncFile(data, globalTemplate, schemaOf(Global{}), remove)
}

// SyncRepo rewrites a <repo>/.kovan.yaml the same way.
func SyncRepo(data []byte, remove map[string]bool) []byte {
	return syncFile(data, repoTemplate, schemaOf(Repo{}), remove)
}

func syncFile(data []byte, template string, schema map[string]nodeKind, remove map[string]bool) []byte {
	user := parseLines(string(data), schema)
	if !hasActiveKeys(user) {
		return []byte(template) // documentation only: no user changes to keep
	}
	m := &merger{
		user:     user,
		tmpl:     parseLines(template, schema),
		schema:   schema,
		remove:   remove,
		consumed: make([]bool, len(user)),
		tmplText: map[string]bool{},
	}
	for _, li := range m.tmpl {
		m.tmplText[li.text] = true
	}
	m.active = map[string]int{}
	for i, li := range user {
		if li.active {
			if _, ok := m.active[li.path]; !ok {
				m.active[li.path] = i
			}
		}
	}
	return []byte(strings.Join(m.run(), "\n"))
}

func hasActiveKeys(lines []lineInfo) bool {
	for _, li := range lines {
		if li.active {
			return true
		}
	}
	return false
}

// merger splices a user's file into the current template: the template is the
// skeleton, user-authored lines land where the template documents their key.
type merger struct {
	user, tmpl []lineInfo
	schema     map[string]nodeKind
	remove     map[string]bool // raw path → drop this dead key
	active     map[string]int  // normalized path → first active user line
	consumed   []bool          // user lines already emitted
	tmplText   map[string]bool // exact template lines, to spot user prose
}

func (m *merger) run() []string {
	var out, pending []string
	section := ""
	for i := 0; i < len(m.tmpl); i++ {
		li := m.tmpl[i]
		if !li.key {
			pending = append(pending, li.text)
			continue
		}
		if top := topLevel(li.path); top != section {
			if section != "" {
				out = append(out, m.keptBlocks(section)...)
			}
			section = top
		}
		out = append(out, pending...)
		pending = nil
		uidx, set := m.active[li.path]
		switch {
		case !set:
			out = append(out, li.text) // fresh documentation
		case m.schema[li.path] == kindStruct:
			out = append(out, m.user[uidx].text) // active section header
			m.consumed[uidx] = true
		default: // leaf, map, or list: the whole user subtree replaces the docs
			out = append(out, m.block(uidx)...)
			i = m.skipDescendants(i)
		}
	}
	if section != "" {
		out = append(out, m.keptBlocks(section)...)
	}
	out = append(out, m.keptBlocks("")...)
	out = append(out, pending...)
	return out
}

func topLevel(path string) string {
	if i := strings.IndexAny(path, ".["); i >= 0 {
		return path[:i]
	}
	return path
}

// block returns a user subtree verbatim: prose comments directly above and
// below it, the key line, everything nested under it, minus dead subtrees
// marked for removal.
func (m *merger) block(i int) []string {
	start, end := m.attachStart(i), m.attachEnd(m.subtreeEnd(i))
	var out []string
	for j := start; j <= end; j++ {
		if m.consumed[j] {
			continue
		}
		if m.user[j].key && m.remove[m.user[j].raw] {
			m.consume(j)
			j = m.subtreeEnd(j)
			continue
		}
		m.consumed[j] = true
		out = append(out, m.user[j].text)
	}
	return out
}

// consume marks a user subtree emitted (or removed) so no later pass repeats it.
func (m *merger) consume(i int) {
	for j := i; j <= m.subtreeEnd(i); j++ {
		m.consumed[j] = true
	}
}

// userProse reports a comment line the current template does not contain and
// that documents no key: a note the user wrote.
func (m *merger) userProse(i int) bool {
	li := m.user[i]
	return li.comment && !li.blank && !li.key && !m.tmplText[li.text]
}

// attachStart extends a user block upward over the prose comments directly
// above it — user notes travel with the line they annotate. Template-authored
// lines and commented keys stay behind; they are refreshed or asked about.
func (m *merger) attachStart(i int) int {
	j := i
	for j-1 >= 0 && m.userProse(j-1) && !m.consumed[j-1] {
		j--
	}
	return j
}

// attachEnd extends a user block downward over the prose comments directly
// below it, unless that run annotates the next active line — then it stays
// with what it documents.
func (m *merger) attachEnd(end int) int {
	j := end
	for j+1 < len(m.user) && m.userProse(j+1) {
		j++
	}
	if j+1 < len(m.user) && m.user[j+1].active {
		return end
	}
	return j
}

// subtreeEnd returns the last line of the subtree rooted at user line i:
// deeper key lines, their interleaved comments and blanks, and deeper
// non-key content (list scalars, wrapped values).
func (m *merger) subtreeEnd(i int) int {
	base, end := m.user[i].indent, i
	for k := i + 1; k < len(m.user); k++ {
		li := m.user[k]
		switch {
		case li.blank || li.comment:
			continue // included only if a deeper line follows
		case li.indent <= base:
			return end
		default:
			end = k
		}
	}
	return end
}

// skipDescendants advances past the template lines a user block replaced: the
// spliced key's documented children plus their trailing continuation comments.
func (m *merger) skipDescendants(i int) int {
	path := m.tmpl[i].path
	last := i
	for k := i + 1; k < len(m.tmpl); k++ {
		li := m.tmpl[k]
		if li.key {
			if !underPath(li.path, path) {
				break
			}
			last = k
			continue
		}
		if li.blank {
			break
		}
	}
	for last+1 < len(m.tmpl) && !m.tmpl[last+1].key && !m.tmpl[last+1].blank {
		last++
	}
	return last
}

func underPath(path, parent string) bool {
	return strings.HasPrefix(path, parent+".") || strings.HasPrefix(path, parent+"[]")
}

// keptBlocks emits the dead keys the user chose to keep, at the end of the
// section they live in; keys under a section the schema no longer has at all
// are emitted at the end of the file (section "").
func (m *merger) keptBlocks(section string) []string {
	var out []string
	for i, li := range m.user {
		if !li.key || m.consumed[i] {
			continue
		}
		if _, known := m.schema[li.path]; known {
			continue
		}
		if m.remove[li.raw] {
			m.consume(i) // the whole dead subtree goes, children included
			continue
		}
		top := topLevel(li.path)
		if _, sectionKnown := m.schema[top]; !sectionKnown {
			top = ""
		}
		if top != section {
			continue
		}
		out = append(out, m.block(i)...)
	}
	return out
}
