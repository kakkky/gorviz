package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Scenario はアプリに何を送るかを書いた宣言的なファイル。負荷のかけ方 (同時実行数・時間・順番) は書かせない
type Scenario struct {
	Target *url.URL
	Ready  Step
	Setup  []Step
	Flows  []Flow
}

type Flow struct {
	Name   string
	Weight int
	Steps  []Step
}

// Step はリクエスト 1 つ。値の中の {{name}} は、前のレスポンスから拾った値 (Capture) と、組み込みの値 (vu・seq) に置き換える
type Step struct {
	Name    string
	Method  string
	Path    string
	Query   map[string]string
	Headers map[string]string
	Body    string
	JSON    any
	Form    map[string]string
	Capture map[string]string
	Expect  []int
}

type scenarioFile struct {
	Target string     `yaml:"target"`
	Ready  *stepFile  `yaml:"ready"`
	Setup  []stepFile `yaml:"setup"`
	Flows  []flowFile `yaml:"flows"`
}

type flowFile struct {
	Name   string     `yaml:"name"`
	Weight *int       `yaml:"weight"`
	Steps  []stepFile `yaml:"steps"`
}

type stepFile struct {
	Name    string            `yaml:"name"`
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Query   map[string]string `yaml:"query"`
	Headers map[string]string `yaml:"headers"`
	Body    string            `yaml:"body"`
	JSON    any               `yaml:"json"`
	Form    map[string]string `yaml:"form"`
	Capture map[string]string `yaml:"capture"`
	Expect  []int             `yaml:"expect"`
}

var (
	methods     = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	placeholder = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)
	builtinVars = []string{"vu", "seq"}
)

// ScenarioProblems はシナリオの誤りをまとめたもの。どこがどう違うかと直し方を 1 行ずつ持つ
type ScenarioProblems []string

func (p ScenarioProblems) Error() string {
	return "the scenario has problems:\n  - " + strings.Join(p, "\n  - ") + "\nsee `gorviz docs scenario` for how to write a scenario"
}

// ParseScenario は YAML のシナリオを読み、負荷をかける前に誤りを返す
func ParseScenario(b []byte) (Scenario, error) {
	var f scenarioFile
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return Scenario{}, ErrInvalidArgument.With(ScenarioProblems{"the file is not a valid scenario: " + err.Error() + "; fix the YAML (unknown keys are rejected)"}.Error())
	}
	var problems ScenarioProblems
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	var s Scenario
	switch u, err := url.Parse(f.Target); {
	case f.Target == "":
		add("target is missing; set it to the base URL the app listens on, like target: http://127.0.0.1:8080")
	case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "":
		add("target %q is not an http(s) URL; write it like http://127.0.0.1:8080", f.Target)
	default:
		s.Target = u
	}
	defined := map[string]bool{}
	for _, v := range builtinVars {
		defined[v] = true
	}
	if f.Ready == nil {
		s.Ready = Step{Method: "GET", Path: "/"}
	} else {
		s.Ready = parseStep(*f.Ready, "ready", defined, add)
	}
	for i, st := range f.Setup {
		s.Setup = append(s.Setup, parseStep(st, fmt.Sprintf("setup[%d]", i), defined, add))
	}
	if len(f.Flows) == 0 {
		add("flows is empty; add at least one flow with steps, the requests sent under load")
	}
	names := map[string]bool{}
	for i, fl := range f.Flows {
		where := fmt.Sprintf("flows[%d]", i)
		flow := Flow{Name: fl.Name, Weight: 1}
		if flow.Name == "" {
			flow.Name = "flow-" + strconv.Itoa(i+1)
		}
		if names[flow.Name] {
			add("%s: the name %q is used twice; give each flow its own name", where, flow.Name)
		}
		names[flow.Name] = true
		if fl.Weight != nil {
			if *fl.Weight < 1 {
				add("%s: weight %d must be 1 or more; it is how often the flow is picked relative to the others", where, *fl.Weight)
			}
			flow.Weight = *fl.Weight
		}
		if len(fl.Steps) == 0 {
			add("%s: steps is empty; add the requests of the flow", where)
		}
		vars := maps.Clone(defined)
		for j, st := range fl.Steps {
			flow.Steps = append(flow.Steps, parseStep(st, fmt.Sprintf("%s.steps[%d]", where, j), vars, add))
		}
		s.Flows = append(s.Flows, flow)
	}
	if len(problems) > 0 {
		return Scenario{}, ErrInvalidArgument.With(problems.Error())
	}
	return s, nil
}

func parseStep(f stepFile, where string, vars map[string]bool, add func(string, ...any)) Step {
	s := Step{Name: f.Name, Method: strings.ToUpper(f.Method), Path: f.Path, Query: f.Query, Headers: f.Headers, Body: f.Body, JSON: f.JSON, Form: f.Form, Capture: f.Capture, Expect: f.Expect}
	if s.Method == "" {
		s.Method = "GET"
	}
	if !slices.Contains(methods, s.Method) {
		add("%s: method %q is not supported; use one of %s", where, f.Method, strings.Join(methods, ", "))
	}
	if !strings.HasPrefix(s.Path, "/") {
		add("%s: path %q must start with /; it is joined to target", where, f.Path)
	}
	bodies := 0
	for _, set := range []bool{f.Body != "", f.JSON != nil, f.Form != nil} {
		if set {
			bodies++
		}
	}
	if bodies > 1 {
		add("%s: use only one of body, json and form", where)
	}
	if f.JSON != nil {
		if _, err := json.Marshal(normalizeYAML(f.JSON)); err != nil {
			add("%s: json cannot be written as JSON: %v", where, err)
		}
		s.JSON = normalizeYAML(f.JSON)
	}
	for _, code := range f.Expect {
		if code < 100 || code > 599 {
			add("%s: expect %d is not an HTTP status", where, code)
		}
	}
	for _, text := range stepTexts(s) {
		for _, m := range placeholder.FindAllStringSubmatch(text, -1) {
			if !vars[m[1]] {
				add("%s: {{%s}} is not captured by an earlier step; capture it first (capture: {%s: json:path}) or use a built-in value (vu, seq)", where, m[1], m[1])
			}
		}
	}
	for name, spec := range f.Capture {
		if !strings.HasPrefix(spec, "json:") && !strings.HasPrefix(spec, "header:") {
			add("%s: capture %s: %q must be json:<path> (like json:id or json:items.0.id) or header:<name> (like header:Location)", where, name, spec)
		}
		vars[name] = true
	}
	return s
}

func stepTexts(s Step) []string {
	out := []string{s.Path, s.Body}
	for _, m := range []map[string]string{s.Query, s.Headers, s.Form} {
		for _, v := range m {
			out = append(out, v)
		}
	}
	if s.JSON != nil {
		b, _ := json.Marshal(s.JSON)
		out = append(out, string(b))
	}
	return out
}

// normalizeYAML は yaml.v3 が map[string]any で読んだものを、JSON に書ける形にそろえる
func normalizeYAML(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, v := range t {
			out[k] = normalizeYAML(v)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, v := range t {
			out[fmt.Sprint(k)] = normalizeYAML(v)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, v := range t {
			out[i] = normalizeYAML(v)
		}
		return out
	}
	return v
}

// Vars はある VU が拾った値
type Vars map[string]string

func (v Vars) expand(s string) string {
	return placeholder.ReplaceAllStringFunc(s, func(m string) string {
		return v[placeholder.FindStringSubmatch(m)[1]]
	})
}

// Request は送るリクエスト 1 つ。Cancel が正なら、その時間でクライアントの側から切る
type Request struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
	Cancel  time.Duration
}

func (s Scenario) Request(st Step, vars Vars) Request {
	u := *s.Target
	u.Path = strings.TrimSuffix(u.Path, "/") + vars.expand(st.Path)
	if p, q, ok := strings.Cut(u.Path, "?"); ok {
		u.Path, u.RawQuery = p, q
	}
	q := u.Query()
	for k, v := range st.Query {
		q.Set(k, vars.expand(v))
	}
	u.RawQuery = q.Encode()
	r := Request{Method: st.Method, URL: u.String(), Headers: map[string]string{}}
	for k, v := range st.Headers {
		r.Headers[k] = vars.expand(v)
	}
	switch {
	case st.JSON != nil:
		b, _ := json.Marshal(st.JSON)
		r.Body = []byte(vars.expand(string(b)))
		if _, ok := r.Headers["Content-Type"]; !ok {
			r.Headers["Content-Type"] = "application/json"
		}
	case st.Form != nil:
		form := url.Values{}
		for k, v := range st.Form {
			form.Set(k, vars.expand(v))
		}
		r.Body = []byte(form.Encode())
		if _, ok := r.Headers["Content-Type"]; !ok {
			r.Headers["Content-Type"] = "application/x-www-form-urlencoded"
		}
	case st.Body != "":
		r.Body = []byte(vars.expand(st.Body))
	}
	return r
}

// Response は送ったリクエストへの応答。Err は応答が得られなかった理由
type Response struct {
	Status   int
	Header   map[string][]string
	Body     []byte
	Latency  time.Duration
	Err      string
	Canceled bool
}

// Passed は、ステップの期待どおりに返ったか。期待を書いていなければ 400 未満
func (st Step) Passed(r Response) bool {
	if r.Err != "" {
		return false
	}
	if len(st.Expect) > 0 {
		return slices.Contains(st.Expect, r.Status)
	}
	return r.Status < 400
}

// CaptureInto はレスポンスから値を拾って vars に足す
func (st Step) CaptureInto(r Response, vars Vars) {
	for name, spec := range st.Capture {
		switch {
		case strings.HasPrefix(spec, "header:"):
			if vs := r.Header[httpHeaderKey(strings.TrimPrefix(spec, "header:"))]; len(vs) > 0 {
				vars[name] = vs[0]
			}
		case strings.HasPrefix(spec, "json:"):
			var v any
			if json.Unmarshal(r.Body, &v) == nil {
				if s, ok := jsonPath(v, strings.TrimPrefix(spec, "json:")); ok {
					vars[name] = s
				}
			}
		}
	}
}

func httpHeaderKey(k string) string {
	parts := strings.Split(strings.ToLower(k), "-")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "-")
}

func jsonPath(v any, path string) (string, bool) {
	for _, key := range strings.Split(path, ".") {
		if key == "" {
			continue
		}
		switch t := v.(type) {
		case map[string]any:
			v = t[key]
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(t) {
				return "", false
			}
			v = t[i]
		default:
			return "", false
		}
	}
	switch t := v.(type) {
	case nil:
		return "", false
	case string:
		return t, true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	default:
		b, _ := json.Marshal(t)
		return string(b), true
	}
}

// PickFlow は重みに沿って flow を選ぶ。r は [0, 1) の乱数
func (s Scenario) PickFlow(r float64) Flow {
	total := 0
	for _, f := range s.Flows {
		total += f.Weight
	}
	n := int(r * float64(total))
	for _, f := range s.Flows {
		if n < f.Weight {
			return f
		}
		n -= f.Weight
	}
	return s.Flows[len(s.Flows)-1]
}
