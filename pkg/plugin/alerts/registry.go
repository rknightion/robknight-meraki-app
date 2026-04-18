package alerts

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// bundledFolderUID is the Grafana folder UID under which every bundled
// alert rule lives. Stable across plugin renames — the reconciler uses
// it to locate the managed folder without having to look up by name.
const bundledFolderUID = "meraki-bundled-folder"

// defaultNoDataState / defaultExecErrState are applied by Render() when
// the YAML does not override them. Keep them conservative: NoData is a
// real signal (the Meraki API genuinely returned nothing) and exec errors
// should surface as alerting rather than being silently swallowed.
const (
	defaultNoDataState  = "NoData"
	defaultExecErrState = "Error"
)

//go:embed templates/*/*.yaml
var templatesFS embed.FS

// Group is a logical grouping of templates (availability, wan, wireless,
// cellular, sensors, cameras, lifecycle). Displayed as a section in the
// bundled-alerts UI.
type Group struct {
	ID          string
	DisplayName string
	Templates   []Template
}

// Template is one alert-rule blueprint. It is immutable after LoadRegistry
// returns — Render() produces a rendered AlertRule copy each call.
type Template struct {
	ID          string
	GroupID     string
	DisplayName string
	Severity    string
	Thresholds  []ThresholdSchema
	// RuleSpec is the raw `rule:` subtree from the YAML. Render() marshals
	// it back to YAML, runs it through text/template, then unmarshals into
	// an AlertRule. Keeping the tree raw means we don't need a second Go
	// shadow of the on-disk shape.
	RuleSpec yaml.Node
}

// ThresholdSchema describes one tunable knob for a template. Default is
// `any` because it might be a duration string, an int, a float, a list
// of strings — the UI layer and Render() branch on Type.
type ThresholdSchema struct {
	Key     string
	Type    string
	Default any
	Label   string
	Help    string
	Options []string
}

// Registry is the loaded, validated set of templates.
type Registry struct {
	groups []Group
	// byID indexes (groupID, templateID) -> Template for O(1) lookup.
	byID map[string]Template
}

// yamlTemplateFile is the on-disk shape. Exported types above are the
// in-memory shape consumed by the rest of the plugin.
type yamlTemplateFile struct {
	Kind        string                `yaml:"kind"`
	ID          string                `yaml:"id"`
	Group       string                `yaml:"group"`
	DisplayName string                `yaml:"display_name"`
	Severity    string                `yaml:"severity"`
	Thresholds  []yamlThresholdSchema `yaml:"thresholds"`
	Rule        yaml.Node             `yaml:"rule"`
}

type yamlThresholdSchema struct {
	Key     string    `yaml:"key"`
	Type    string    `yaml:"type"`
	Default yaml.Node `yaml:"default"`
	Label   string    `yaml:"label"`
	Help    string    `yaml:"help"`
	Options []string  `yaml:"options"`
}

// LoadRegistry reads the embedded templates/*/*.yaml tree and returns a
// populated Registry. It is the production entry point; tests that want
// a different filesystem should use LoadRegistryFS directly.
func LoadRegistry() (*Registry, error) {
	sub, err := fs.Sub(templatesFS, "templates")
	if err != nil {
		return nil, fmt.Errorf("alerts: sub-FS: %w", err)
	}
	return LoadRegistryFS(sub)
}

// LoadRegistryFS loads templates from an arbitrary fs.FS rooted at the
// group directory level (i.e. the FS has `availability/foo.yaml` at its
// top). Exposed so tests can inject fixtures via testing/fstest.MapFS.
func LoadRegistryFS(fsys fs.FS) (*Registry, error) {
	reg := &Registry{byID: map[string]Template{}}
	groupByID := map[string]*Group{}

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		raw, rerr := fs.ReadFile(fsys, path)
		if rerr != nil {
			return fmt.Errorf("read %s: %w", path, rerr)
		}
		var ytf yamlTemplateFile
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if derr := dec.Decode(&ytf); derr != nil {
			return fmt.Errorf("decode %s: %w", path, derr)
		}
		if ytf.Kind != "alert_rule_template" {
			return fmt.Errorf("%s: expected kind=alert_rule_template, got %q", path, ytf.Kind)
		}
		if ytf.ID == "" || ytf.Group == "" {
			return fmt.Errorf("%s: id and group are required", path)
		}
		key := ytf.Group + "/" + ytf.ID
		if _, dup := reg.byID[key]; dup {
			return fmt.Errorf("duplicate template %s (loaded from %s)", key, path)
		}

		thresholds := make([]ThresholdSchema, 0, len(ytf.Thresholds))
		for _, yt := range ytf.Thresholds {
			def, cerr := decodeThresholdDefault(yt.Type, yt.Default)
			if cerr != nil {
				return fmt.Errorf("%s threshold %q: %w", path, yt.Key, cerr)
			}
			thresholds = append(thresholds, ThresholdSchema{
				Key:     yt.Key,
				Type:    yt.Type,
				Default: def,
				Label:   yt.Label,
				Help:    yt.Help,
				Options: yt.Options,
			})
		}

		tpl := Template{
			ID:          ytf.ID,
			GroupID:     ytf.Group,
			DisplayName: ytf.DisplayName,
			Severity:    ytf.Severity,
			Thresholds:  thresholds,
			RuleSpec:    ytf.Rule,
		}

		g, ok := groupByID[ytf.Group]
		if !ok {
			g = &Group{ID: ytf.Group, DisplayName: humanize(ytf.Group)}
			groupByID[ytf.Group] = g
		}
		g.Templates = append(g.Templates, tpl)
		reg.byID[key] = tpl
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Flatten groupByID into a stable, deterministic slice. Group order is
	// currently insertion-ordered from WalkDir (lexical by path) which is
	// good enough — fs.WalkDir walks in lexical order.
	for _, g := range groupByID {
		reg.groups = append(reg.groups, *g)
	}
	// Sort by group ID so Groups() is deterministic regardless of FS impl.
	sortGroupsByID(reg.groups)
	return reg, nil
}

// Groups returns all groups in deterministic order.
func (r *Registry) Groups() []Group { return r.groups }

// Group looks up a single group by ID. The bool is false if not found.
func (r *Registry) Group(id string) (Group, bool) {
	for _, g := range r.groups {
		if g.ID == id {
			return g, true
		}
	}
	return Group{}, false
}

// Template looks up a single template by (groupID, templateID).
func (r *Registry) Template(groupID, templateID string) (Template, bool) {
	t, ok := r.byID[groupID+"/"+templateID]
	return t, ok
}

// renderContext is the object exposed to the YAML text/template layer.
// Keep the field names stable — they're referenced by every template on
// disk and renaming them is a breaking change.
type renderContext struct {
	OrgID      string
	Thresholds map[string]any
}

// Render produces a concrete AlertRule by:
//  1. cloning the template's effective threshold map (defaults + overrides);
//  2. marshalling the RuleSpec yaml.Node back to text;
//  3. running it through text/template with a renderContext;
//  4. unmarshalling the result into an AlertRule;
//  5. backfilling FolderUID, RuleGroup, UID and default states.
//
// UID is deterministic: `meraki-<group>-<template>-<org>`. The reconciler
// trusts this UID — changing the format is a breaking change for any
// existing installs.
func (t Template) Render(orgID string, overrides map[string]any) (AlertRule, error) {
	if orgID == "" {
		return AlertRule{}, errors.New("alerts: orgID is required")
	}

	// Build effective threshold map: defaults first, overrides win.
	thresholds := make(map[string]any, len(t.Thresholds))
	for _, th := range t.Thresholds {
		thresholds[th.Key] = th.Default
	}
	for k, v := range overrides {
		thresholds[k] = v
	}

	// Marshal the rule subtree back to YAML so we can run text/template
	// across the whole block in one shot. yaml.Marshal on a *yaml.Node
	// preserves scalar quoting well enough for our substitutions.
	raw, err := yaml.Marshal(&t.RuleSpec)
	if err != nil {
		return AlertRule{}, fmt.Errorf("alerts: marshal rule spec: %w", err)
	}

	tmpl, err := template.New(t.GroupID + "/" + t.ID).
		// Delimiters are `<% ... %>` instead of the Go default `{{ ... }}`
		// so template actions don't collide with YAML's flow-mapping
		// syntax (`{ foo: bar }`). The on-disk YAML is valid YAML even
		// before template execution, which means yaml.Node can parse it
		// without interpreting our substitutions as flow mappings.
		Delims("<%", "%>").
		Option("missingkey=error").
		Funcs(template.FuncMap{
			// yamlList renders a list threshold as a YAML flow sequence
			// (`[a, b, c]`) which is valid both as a bare YAML value and,
			// once the whole tree is JSON-marshalled, as a JSON array.
			// Use this for any list threshold rendered into a bare YAML
			// value — NOT inside a quoted string.
			"yamlList": yamlListFormat,
		}).
		Parse(string(raw))
	if err != nil {
		return AlertRule{}, fmt.Errorf("alerts: parse rule template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, renderContext{
		OrgID:      orgID,
		Thresholds: thresholds,
	}); err != nil {
		return AlertRule{}, fmt.Errorf("alerts: execute rule template: %w", err)
	}

	// Decode the rendered YAML into a generic map, JSON-marshal it, then
	// JSON-unmarshal into AlertRule. This two-hop conversion gets us
	// "the AlertRule struct as the canonical shape" without having to
	// mirror the YAML keys twice.
	var generic map[string]any
	if err := yaml.Unmarshal(buf.Bytes(), &generic); err != nil {
		return AlertRule{}, fmt.Errorf("alerts: decode rendered rule: %w", err)
	}
	normaliseYAMLMap(generic)
	jsonBytes, err := json.Marshal(generic)
	if err != nil {
		return AlertRule{}, fmt.Errorf("alerts: marshal rendered rule: %w", err)
	}
	var rule AlertRule
	if err := json.Unmarshal(jsonBytes, &rule); err != nil {
		return AlertRule{}, fmt.Errorf("alerts: unmarshal rendered rule: %w", err)
	}

	if rule.NoDataState == "" {
		rule.NoDataState = defaultNoDataState
	}
	if rule.ExecErrState == "" {
		rule.ExecErrState = defaultExecErrState
	}
	rule.FolderUID = bundledFolderUID
	rule.RuleGroup = t.GroupID
	rule.UID = fmt.Sprintf("meraki-%s-%s-%s", t.GroupID, t.ID, orgID)

	return rule, nil
}

// decodeThresholdDefault converts a raw yaml.Node default into a concrete
// Go value based on the threshold's declared type. We intentionally keep
// this permissive — the UI layer will validate user overrides against the
// same schema, so producing e.g. `"5m"` for a duration and `[]string{...}`
// for a list is enough.
func decodeThresholdDefault(typ string, node yaml.Node) (any, error) {
	// Zero node = no default; Render() will see a missing key and the
	// template will fail with a helpful error.
	if node.Kind == 0 {
		return nil, nil
	}
	switch typ {
	case "int":
		var v int
		if err := node.Decode(&v); err != nil {
			return nil, fmt.Errorf("int default: %w", err)
		}
		return v, nil
	case "float":
		var v float64
		if err := node.Decode(&v); err != nil {
			return nil, fmt.Errorf("float default: %w", err)
		}
		return v, nil
	case "string", "duration":
		var v string
		if err := node.Decode(&v); err != nil {
			return nil, fmt.Errorf("string default: %w", err)
		}
		return v, nil
	case "list":
		var v []string
		if err := node.Decode(&v); err != nil {
			return nil, fmt.Errorf("list default: %w", err)
		}
		return v, nil
	default:
		// Unknown type — decode to `any` and trust the template author.
		var v any
		if err := node.Decode(&v); err != nil {
			return nil, fmt.Errorf("generic default: %w", err)
		}
		return v, nil
	}
}

// yamlListFormat renders a list threshold as a JSON/YAML flow sequence
// with double-quoted string items — e.g. `["a", "b", "c"]`. Double
// quoting is deliberate: it keeps the emitted YAML robust against items
// that happen to look like bools or numbers ("true", "1").
func yamlListFormat(v any) string {
	// Convert to []string regardless of the concrete container — YAML
	// list defaults may come back as []string OR []any depending on the
	// decoding path.
	var items []string
	switch x := v.(type) {
	case []string:
		items = x
	case []any:
		items = make([]string, 0, len(x))
		for _, e := range x {
			items = append(items, fmt.Sprintf("%v", e))
		}
	default:
		// Fall back to JSON encoding of whatever we got — gives us a
		// deterministic, YAML-compatible representation.
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		// json.Marshal gives us a correctly-escaped double-quoted string.
		q, _ := json.Marshal(s)
		b.Write(q)
	}
	b.WriteByte(']')
	return b.String()
}

// normaliseYAMLMap walks a decoded YAML tree and converts map[any]any
// (which yaml.v3 produces for top-level maps) into map[string]any so
// the subsequent json.Marshal succeeds.
func normaliseYAMLMap(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, inner := range x {
			x[k] = normaliseYAMLMap(inner)
		}
		return x
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, inner := range x {
			out[fmt.Sprintf("%v", k)] = normaliseYAMLMap(inner)
		}
		return out
	case []any:
		for i, inner := range x {
			x[i] = normaliseYAMLMap(inner)
		}
		return x
	default:
		return v
	}
}

func humanize(id string) string {
	if id == "" {
		return ""
	}
	// wan -> WAN, wireless -> Wireless. Two-letter acronyms uppercase;
	// everything else title-cases the first rune.
	if len(id) <= 3 {
		return strings.ToUpper(id)
	}
	return strings.ToUpper(id[:1]) + id[1:]
}

func sortGroupsByID(groups []Group) {
	// Simple insertion sort — the slice is tiny (<10) and we avoid pulling
	// in sort just to get a stable order.
	for i := 1; i < len(groups); i++ {
		for j := i; j > 0 && groups[j-1].ID > groups[j].ID; j-- {
			groups[j-1], groups[j] = groups[j], groups[j-1]
		}
	}
}
