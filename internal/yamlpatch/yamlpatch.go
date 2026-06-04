// Package yamlpatch sets values inside a YAML document by a dotted path while
// preserving comments and key ordering. It exists so srv never builds YAML by
// textual substitution of (possibly untrusted) values into a template string —
// a value spliced through Set is YAML-encoded by the marshaller, so a quote,
// newline, or `key: value` payload in the input cannot break the document
// structure or inject sibling keys. Mirrors the same-named package in treeman.
package yamlpatch

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Segment is one step in a dotted path. Exactly one of Key or (IsIndex + Idx)
// is populated.
type Segment struct {
	Key     string
	Idx     int
	IsIndex bool
}

// ParsePath tokenises "a.b[0].c" → [a, b, [0], c]. Bracket indices may chain
// (e.g. "x[0][1]") to walk nested sequences.
func ParsePath(p string) ([]Segment, error) {
	if p == "" {
		return nil, errors.New("path is empty")
	}
	var segs []Segment
	for part := range strings.SplitSeq(p, ".") {
		if part == "" {
			return nil, fmt.Errorf("empty segment in path %q", p)
		}
		for {
			i := strings.Index(part, "[")
			if i < 0 {
				if part != "" {
					segs = append(segs, Segment{Key: part})
				}
				break
			}
			if i > 0 {
				segs = append(segs, Segment{Key: part[:i]})
			}
			j := strings.Index(part[i:], "]")
			if j < 0 {
				return nil, fmt.Errorf("unclosed '[' in path %q", p)
			}
			j += i
			n, err := strconv.Atoi(part[i+1 : j])
			if err != nil {
				return nil, fmt.Errorf("non-integer index %q in path %q", part[i+1:j], p)
			}
			segs = append(segs, Segment{IsIndex: true, Idx: n})
			part = part[j+1:]
		}
	}
	return segs, nil
}

// Set walks the YAML AST in root to segs and replaces the terminal node with
// newVal. Missing intermediate MAPPING keys are created. SEQUENCE indices are
// never extended — an out-of-range index is an error so callers cannot silently
// reshape a list. Returns the previous node (or nil if a new key was created).
func Set(root *yaml.Node, segs []Segment, newVal *yaml.Node) (prev *yaml.Node, err error) {
	cur := root
	if cur.Kind == yaml.DocumentNode {
		if len(cur.Content) == 0 {
			cur.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
		}
		cur = cur.Content[0]
	}
	for i, seg := range segs {
		last := i == len(segs)-1
		if seg.IsIndex {
			if cur.Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("segment %d: expected sequence at this position, got %s", i, KindName(cur.Kind))
			}
			if seg.Idx < 0 || seg.Idx >= len(cur.Content) {
				return nil, fmt.Errorf("segment %d: index %d out of range (len=%d)", i, seg.Idx, len(cur.Content))
			}
			if last {
				prev = cur.Content[seg.Idx]
				cur.Content[seg.Idx] = newVal
				return prev, nil
			}
			cur = cur.Content[seg.Idx]
			continue
		}
		if cur.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("segment %d (%q): expected mapping at this position, got %s", i, seg.Key, KindName(cur.Kind))
		}
		idx := -1
		for k := 0; k < len(cur.Content); k += 2 {
			if cur.Content[k].Value == seg.Key {
				idx = k
				break
			}
		}
		if idx < 0 {
			if last {
				cur.Content = append(cur.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Value: seg.Key},
					newVal,
				)
				return nil, nil
			}
			cur.Content = append(cur.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: seg.Key},
				&yaml.Node{Kind: yaml.MappingNode},
			)
			cur = cur.Content[len(cur.Content)-1]
			continue
		}
		if last {
			prev = cur.Content[idx+1]
			cur.Content[idx+1] = newVal
			return prev, nil
		}
		cur = cur.Content[idx+1]
	}
	return nil, nil
}

// ValueToNode round-trips any value through yaml.Marshal → yaml.Unmarshal so
// the result is a proper *yaml.Node suitable for splicing into the AST. This is
// where injection safety comes from: the value becomes an encoded scalar node,
// not raw text concatenated into the document.
func ValueToNode(v any) (*yaml.Node, error) {
	b, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0], nil
	}
	return &doc, nil
}

// SetPath is the convenience wrapper: parse the dotted path, encode value as a
// node, and Set it into root. The common case for callers that just want to
// place a scalar safely.
func SetPath(root *yaml.Node, path string, value any) error {
	segs, err := ParsePath(path)
	if err != nil {
		return err
	}
	node, err := ValueToNode(value)
	if err != nil {
		return err
	}
	_, err = Set(root, segs, node)
	return err
}

// Marshal renders a yaml.Node tree back to bytes with two-space indent.
func Marshal(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// KindName returns a human-readable label for a yaml.Kind.
func KindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	}
	return "unknown"
}
