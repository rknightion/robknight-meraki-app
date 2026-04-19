// Package recordings bundles the Grafana-managed recording-rule registry and
// (in later phases) the reconciler that provisions those rules into Grafana
// via the shared provisioning HTTP API.
//
// This package deliberately mirrors the shape of `pkg/plugin/alerts`. The two
// reuse the same wire struct (`alerts.AlertRule`) — Grafana's provisioning
// endpoint accepts both alert and recording rules on the same path, and the
// presence of a non-nil `Record` block is what distinguishes them. The label
// gate `meraki_kind=recording` plus the `meraki-rec-` UID prefix keep the two
// reconcilers from stepping on each other. See `CLAUDE.md` for the full
// invariant list.
package recordings

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/robknight/grafana-meraki-plugin/pkg/plugin/alerts"
)

// bundledRecordingsFolderUID is the Grafana folder UID under which every
// bundled recording rule lives. Stable across plugin renames — the
// reconciler uses it to locate the managed folder without a name lookup.
const bundledRecordingsFolderUID = "meraki-bundled-rec-folder"

// BundledRecordingsFolderUID exposes the folder UID for use by the
// resource layer + reconciler in the parent plugin package.
func BundledRecordingsFolderUID() string { return bundledRecordingsFolderUID }

// recordingTemplateKind is the top-level `kind:` value required on every
// YAML template file. Anything else is rejected at load time.
const recordingTemplateKind = "recording_rule_template"

// metricNameRE enforces the `meraki_<group>_<name>` naming contract. The
// Prometheus metric-name grammar is broader; this constraint keeps emitted
// series easy to discover and filter in dashboards.
var metricNameRE = regexp.MustCompile(`^meraki_[a-z][a-z0-9_]*$`)

//go:embed templates/*/*.yaml
var templatesFS embed.FS

// Group is a logical grouping of recording templates (availability, wan,
// wireless, ...). Displayed as a section in the bundled-recordings UI.
type Group struct {
	ID          string
	DisplayName string
	Templates   []Template
}

// Template is one recording-rule blueprint. It is immutable after
// LoadRegistry returns — Render() produces a fresh AlertRule per call.
type Template struct {
	ID          string
	GroupID     string
	DisplayName string
	// Metric is the Prometheus series name this rule will emit. Resolved
	// at load time from the template YAML's `rule.record.metric` key so
	// the UI can surface it without re-rendering.
	Metric     string
	Thresholds []ThresholdSchema
	// RuleSpec is the raw `rule:` subtree from the YAML. Render() marshals
	// it back to YAML, runs it through text/template, then unmarshals into
	// an alerts.AlertRule. Keeping the tree raw means we don't need a
	// second Go shadow of the on-disk shape.
	RuleSpec yaml.Node
}

// ThresholdSchema describes one tunable knob for a template. Same shape as
// alerts.ThresholdSchema — intentionally duplicated here rather than
// imported so the recordings public surface doesn't couple consumers to the
// alerts package.
type ThresholdSchema struct {
	Key     string
	Type    string
	Default any
	Label   string
	Help    string
	Options []string
}

// Registry is the loaded, validated set of recording-rule templates.
type Registry struct {
	groups []Group
	byID   map[string]Template
}

// yamlTemplateFile is the on-disk shape for one recording-rule template.
type yamlTemplateFile struct {
	Kind        string                `yaml:"kind"`
	ID          string                `yaml:"id"`
	Group       string                `yaml:"group"`
	DisplayName string                `yaml:"display_name"`
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
// populated Registry. Production entry point; tests that want to feed a
// synthetic FS should call LoadRegistryFS directly.
func LoadRegistry() (*Registry, error) {
	sub, err := fs.Sub(templatesFS, "templates")
	if err != nil {
		return nil, fmt.Errorf("recordings: sub-FS: %w", err)
	}
	return LoadRegistryFS(sub)
}

// LoadRegistryFS loads templates from an arbitrary fs.FS rooted at the group
// directory level (so the FS has `availability/foo.yaml` at its top).
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
		if ytf.Kind != recordingTemplateKind {
			return fmt.Errorf("%s: expected kind=%s, got %q", path, recordingTemplateKind, ytf.Kind)
		}
		if ytf.ID == "" || ytf.Group == "" {
			return fmt.Errorf("%s: id and group are required", path)
		}
		key := ytf.Group + "/" + ytf.ID
		if _, dup := reg.byID[key]; dup {
			return fmt.Errorf("duplicate template %s (loaded from %s)", key, path)
		}

		metric, merr := extractMetricName(ytf.Rule)
		if merr != nil {
			return fmt.Errorf("%s: %w", path, merr)
		}
		if !metricNameRE.MatchString(metric) {
			return fmt.Errorf("%s: metric %q does not match %s", path, metric, metricNameRE)
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
			Metric:      metric,
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

	for _, g := range groupByID {
		reg.groups = append(reg.groups, *g)
	}
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

// renderContext is exposed to the YAML text/template layer. Field names are
// stable — templates on disk reference them and renaming breaks things.
type renderContext struct {
	OrgID      string
	Thresholds map[string]any
}

// Render produces a concrete alerts.AlertRule representing one recording
// rule for the given orgID + operator overrides. It:
//
//  1. merges defaults + overrides into the effective threshold map;
//  2. re-marshals the RuleSpec yaml.Node back to text;
//  3. runs it through text/template with `<% %>` delimiters;
//  4. decodes the rendered YAML → generic map → JSON → AlertRule;
//  5. backfills FolderUID, RuleGroup, UID, For="0s", and
//     Record.TargetDatasourceUID.
//
// Render is pure: same (orgID, overrides, targetDsUID) → same AlertRule
// (modulo map iteration order, which we lean on encoding/json to make
// deterministic via struct ordering).
//
// Render does NOT set Condition, NoDataState, or ExecErrState — those
// fields are alert-specific and are forbidden on recording-rule
// submissions. The `omitempty` tags on alerts.AlertRule ensure they drop
// from the serialised payload.
func (t Template) Render(orgID string, overrides map[string]any, targetDsUID string) (alerts.AlertRule, error) {
	if orgID == "" {
		return alerts.AlertRule{}, errors.New("recordings: orgID is required")
	}
	if targetDsUID == "" {
		return alerts.AlertRule{}, errors.New("recordings: targetDsUID is required — reconcile must supply the operator-selected Prometheus DS UID")
	}

	thresholds := make(map[string]any, len(t.Thresholds))
	for _, th := range t.Thresholds {
		thresholds[th.Key] = th.Default
	}
	for k, v := range overrides {
		thresholds[k] = v
	}

	raw, err := yaml.Marshal(&t.RuleSpec)
	if err != nil {
		return alerts.AlertRule{}, fmt.Errorf("recordings: marshal rule spec: %w", err)
	}

	tmpl, err := template.New(t.GroupID + "/" + t.ID).
		Delims("<%", "%>").
		Option("missingkey=error").
		Funcs(template.FuncMap{"yamlList": yamlListFormat}).
		Parse(string(raw))
	if err != nil {
		return alerts.AlertRule{}, fmt.Errorf("recordings: parse rule template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, renderContext{
		OrgID:      orgID,
		Thresholds: thresholds,
	}); err != nil {
		return alerts.AlertRule{}, fmt.Errorf("recordings: execute rule template: %w", err)
	}

	var generic map[string]any
	if err := yaml.Unmarshal(buf.Bytes(), &generic); err != nil {
		return alerts.AlertRule{}, fmt.Errorf("recordings: decode rendered rule: %w", err)
	}
	normaliseYAMLMap(generic)
	jsonBytes, err := json.Marshal(generic)
	if err != nil {
		return alerts.AlertRule{}, fmt.Errorf("recordings: marshal rendered rule: %w", err)
	}
	var rule alerts.AlertRule
	if err := json.Unmarshal(jsonBytes, &rule); err != nil {
		return alerts.AlertRule{}, fmt.Errorf("recordings: unmarshal rendered rule: %w", err)
	}

	if rule.Record == nil {
		return alerts.AlertRule{}, fmt.Errorf("recordings: template %s/%s rendered without a record block", t.GroupID, t.ID)
	}
	rule.Record.TargetDatasourceUID = targetDsUID
	rule.FolderUID = bundledRecordingsFolderUID
	rule.RuleGroup = t.GroupID
	rule.UID = fmt.Sprintf("meraki-rec-%s-%s-%s", t.GroupID, t.ID, orgID)
	rule.For = "0s"

	return rule, nil
}

// extractMetricName pulls `rule.record.metric` out of the raw RuleSpec
// yaml.Node so LoadRegistry can validate the metric name without having to
// fully render the template (which requires a concrete orgID). Errors when
// the record block is absent or the metric field is missing / non-string.
func extractMetricName(rule yaml.Node) (string, error) {
	if rule.Kind != yaml.MappingNode {
		return "", errors.New("rule: expected a mapping node")
	}
	recordNode := findMapChild(&rule, "record")
	if recordNode == nil {
		return "", errors.New("rule: record block is required for recording templates")
	}
	if recordNode.Kind != yaml.MappingNode {
		return "", errors.New("rule.record: expected a mapping node")
	}
	metricNode := findMapChild(recordNode, "metric")
	if metricNode == nil || metricNode.Kind != yaml.ScalarNode {
		return "", errors.New("rule.record.metric: required scalar")
	}
	return metricNode.Value, nil
}

// findMapChild looks up a string key in a YAML mapping node. Returns nil
// when absent. Only used for load-time validation, so performance of the
// linear scan is irrelevant.
func findMapChild(mapping *yaml.Node, key string) *yaml.Node {
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		k := mapping.Content[i]
		v := mapping.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return v
		}
	}
	return nil
}

// decodeThresholdDefault converts a raw yaml.Node default into a concrete
// Go value based on the declared threshold type. Mirrors
// alerts.decodeThresholdDefault exactly — keep the two in sync.
func decodeThresholdDefault(typ string, node yaml.Node) (any, error) {
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
		var v any
		if err := node.Decode(&v); err != nil {
			return nil, fmt.Errorf("generic default: %w", err)
		}
		return v, nil
	}
}

// yamlListFormat renders a list threshold as a JSON/YAML flow sequence
// with double-quoted string items. Kept in sync with
// alerts.yamlListFormat.
func yamlListFormat(v any) string {
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
		q, _ := json.Marshal(s)
		b.Write(q)
	}
	b.WriteByte(']')
	return b.String()
}

// normaliseYAMLMap walks a decoded YAML tree and converts map[any]any
// (which yaml.v3 produces for top-level maps) into map[string]any so
// the subsequent json.Marshal succeeds. Mirrors alerts.normaliseYAMLMap.
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
	if len(id) <= 3 {
		return strings.ToUpper(id)
	}
	return strings.ToUpper(id[:1]) + id[1:]
}

func sortGroupsByID(groups []Group) {
	for i := 1; i < len(groups); i++ {
		for j := i; j > 0 && groups[j-1].ID > groups[j].ID; j-- {
			groups[j-1], groups[j] = groups[j], groups[j-1]
		}
	}
}
