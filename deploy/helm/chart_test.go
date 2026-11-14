package helm

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/amareshhebbar/rungated/internal/config"
	"github.com/amareshhebbar/rungated/pkg/manifest"
)

const chartDir = "rungated"

type files struct{}

func (files) Get(name string) string {
	b, err := os.ReadFile(filepath.Join(chartDir, name))
	if err != nil {
		return ""
	}
	return string(b)
}

func deepMerge(dst, src map[string]any) map[string]any {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				dst[k] = deepMerge(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
	return dst
}

func empty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String, reflect.Map, reflect.Slice, reflect.Array:
		return rv.Len() == 0
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int64, reflect.Int32:
		return rv.Int() == 0
	case reflect.Float64, reflect.Float32:
		return rv.Float() == 0
	}
	return false
}

func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}

func render(t *testing.T, overrides string) (map[string]string, error) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(chartDir, "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	if overrides != "" {
		ov := map[string]any{}
		if err := yaml.Unmarshal([]byte(overrides), &ov); err != nil {
			t.Fatal(err)
		}
		values = deepMerge(values, ov)
	}
	root := template.New("root")
	funcs := template.FuncMap{
		"include": func(name string, data any) (string, error) {
			var b bytes.Buffer
			tpl := root.Lookup(name)
			if tpl == nil {
				return "", fmt.Errorf("no template %q", name)
			}
			err := tpl.Execute(&b, data)
			return b.String(), err
		},
		"toYaml": func(v any) (string, error) {
			b, err := yaml.Marshal(v)
			return strings.TrimSuffix(string(b), "\n"), err
		},
		"nindent": func(n int, s string) string {
			pad := strings.Repeat(" ", n)
			return "\n" + pad + strings.ReplaceAll(s, "\n", "\n"+pad)
		},
		"indent": func(n int, s string) string {
			pad := strings.Repeat(" ", n)
			return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
		},
		"quote": func(v any) string { return fmt.Sprintf("%q", fmt.Sprint(v)) },
		"trunc": func(n int, s string) string {
			if len(s) > n {
				return s[:n]
			}
			return s
		},
		"trimSuffix": func(suffix, s string) string { return strings.TrimSuffix(s, suffix) },
		"contains":   func(sub, s string) bool { return strings.Contains(s, sub) },
		"replace":    func(old, new, s string) string { return strings.ReplaceAll(s, old, new) },
		"default": func(def any, given ...any) any {
			if len(given) == 0 || empty(given[0]) {
				return def
			}
			return given[0]
		},
		"sha256sum": func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) },
		"add":       func(a, b any) int { return toInt(a) + toInt(b) },
		"int":       toInt,
		"fail":      func(msg string) (string, error) { return "", fmt.Errorf("%s", msg) },
	}
	root.Funcs(funcs).Option("missingkey=zero")
	names, _ := filepath.Glob(filepath.Join(chartDir, "templates", "*"))
	for _, p := range names {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := root.New("rungated/templates/" + filepath.Base(p)).Parse(string(body)); err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
	}
	data := map[string]any{
		"Values":   values,
		"Release":  map[string]any{"Name": "ab", "Namespace": "sec", "Service": "Helm"},
		"Chart":    map[string]any{"Name": "rungated", "Version": "0.10.0", "AppVersion": "0.10.0"},
		"Template": map[string]any{"BasePath": "rungated/templates"},
		"Files":    files{},
	}
	out := map[string]string{}
	for _, p := range names {
		base := filepath.Base(p)
		if base == "_helpers.tpl" {
			continue
		}
		var b bytes.Buffer
		if err := root.Lookup("rungated/templates/"+base).Execute(&b, data); err != nil {
			return nil, err
		}
		out[base] = b.String()
	}
	return out, nil
}

type doc = map[string]any

func docs(t *testing.T, name, body string) []doc {
	t.Helper()
	var out []doc
	for _, part := range strings.Split(body, "\n---\n") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		var d doc
		if err := yaml.Unmarshal([]byte(part), &d); err != nil {
			t.Fatalf("%s is not valid yaml: %v\n%s", name, err, part)
		}
		if len(d) > 0 {
			out = append(out, d)
		}
	}
	return out
}

func pick(t *testing.T, v any, path ...string) any {
	t.Helper()
	cur := v
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("path %v: %v is not a map", path, cur)
		}
		cur = m[k]
	}
	return cur
}

func all(t *testing.T, out map[string]string) map[string][]doc {
	t.Helper()
	res := map[string][]doc{}
	for name, body := range out {
		if strings.HasSuffix(name, "NOTES.txt") {
			continue
		}
		for _, d := range docs(t, name, body) {
			kind, _ := d["kind"].(string)
			if kind == "" {
				t.Fatalf("%s produced a document without kind: %v", name, d)
			}
			res[kind] = append(res[kind], d)
		}
	}
	return res
}

func writeConfig(t *testing.T, out map[string]string) string {
	t.Helper()
	cm := docs(t, "configmap.yaml", out["configmap.yaml"])[0]
	body := pick(t, cm, "data", "rungated.yaml").(string)
	p := filepath.Join(t.TempDir(), "rungated.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultRender(t *testing.T) {
	out, err := render(t, "")
	if err != nil {
		t.Fatal(err)
	}
	k := all(t, out)
	for _, kind := range []string{"Deployment", "Service", "ServiceAccount", "PersistentVolumeClaim", "ConfigMap"} {
		if len(k[kind]) == 0 {
			t.Errorf("missing %s", kind)
		}
	}
	if len(k["ConfigMap"]) != 2 {
		t.Errorf("expected two config maps, got %d", len(k["ConfigMap"]))
	}
	for _, kind := range []string{"Secret", "Ingress", "HorizontalPodAutoscaler", "PodDisruptionBudget", "NetworkPolicy", "ServiceMonitor"} {
		if len(k[kind]) != 0 {
			t.Errorf("%s should not render by default", kind)
		}
	}
	dep := k["Deployment"][0]
	if pick(t, dep, "spec", "strategy", "type") != "Recreate" {
		t.Error("embedded store must use Recreate")
	}
	if pick(t, dep, "spec", "replicas") != 1 {
		t.Errorf("replicas %v", pick(t, dep, "spec", "replicas"))
	}
	container := pick(t, dep, "spec", "template", "spec", "containers").([]any)[0].(map[string]any)
	if container["image"] != "ghcr.io/amareshhebbar/rungated:0.10.0" {
		t.Errorf("image %v", container["image"])
	}
	sec := container["securityContext"].(map[string]any)
	if sec["readOnlyRootFilesystem"] != true || sec["allowPrivilegeEscalation"] != false {
		t.Errorf("container security context: %v", sec)
	}
	mounts := map[string]bool{}
	for _, m := range container["volumeMounts"].([]any) {
		mounts[m.(map[string]any)["name"].(string)] = true
	}
	for _, v := range pick(t, dep, "spec", "template", "spec", "volumes").([]any) {
		delete(mounts, v.(map[string]any)["name"].(string))
	}
	if len(mounts) != 0 {
		t.Errorf("volume mounts without volumes: %v", mounts)
	}
	ann := pick(t, dep, "spec", "template", "metadata", "annotations").(map[string]any)
	if len(ann["checksum/config"].(string)) != 64 {
		t.Errorf("checksum annotation: %v", ann)
	}
	svc := k["Service"][0]
	sel := pick(t, svc, "spec", "selector").(map[string]any)
	tplLabels := pick(t, dep, "spec", "template", "metadata", "labels").(map[string]any)
	matchLabels := pick(t, dep, "spec", "selector", "matchLabels").(map[string]any)
	for key, v := range sel {
		if tplLabels[key] != v || matchLabels[key] != v {
			t.Errorf("selector label %s=%v is not on the pod template", key, v)
		}
	}
	ports := map[string]bool{}
	for _, p := range container["ports"].([]any) {
		ports[p.(map[string]any)["name"].(string)] = true
	}
	for _, p := range pick(t, svc, "spec", "ports").([]any) {
		if !ports[p.(map[string]any)["targetPort"].(string)] {
			t.Errorf("service targets unknown container port %v", p)
		}
	}
	cfg, err := config.Load(writeConfig(t, out))
	if err != nil {
		t.Fatalf("rendered rungated.yaml does not load: %v", err)
	}
	if cfg.Listen != ":8080" || cfg.EgressListen != ":8081" || cfg.DataDir != "/data" || cfg.Store.Kind != "embedded" || !cfg.Auth.RequireAgentAuth {
		t.Errorf("unexpected config %+v", cfg)
	}
	if len(cfg.Manifests) != 1 || cfg.Manifests[0] != "/etc/rungated/manifests" {
		t.Errorf("manifests %v", cfg.Manifests)
	}
	if cfg.Upstreams.OpenAI.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("upstream %+v", cfg.Upstreams.OpenAI)
	}
}

func TestPostgresScaleOut(t *testing.T) {
	t.Setenv("RUNGATED_DATABASE_URL", "postgres://u:p@db/ab")
	out, err := render(t, `
replicaCount: 3
config:
  store: {kind: postgres}
  bus: {kind: nats, url: "nats://nats:4222"}
secrets:
  apiKey: admin-key
  databaseUrl: "postgres://u:p@db/ab"
  openaiApiKey: sk-test
autoscaling: {enabled: true}
podDisruptionBudget: {enabled: true}
persistence: {enabled: true}
`)
	if err != nil {
		t.Fatal(err)
	}
	k := all(t, out)
	if len(k["PersistentVolumeClaim"]) != 0 {
		t.Error("postgres deployments must not create a data volume claim")
	}
	dep := k["Deployment"][0]
	if pick(t, dep, "spec", "strategy") != nil {
		t.Error("postgres deployments should use the default rolling update")
	}
	if pick(t, dep, "spec", "replicas") != nil {
		t.Error("replicas must be left to the autoscaler")
	}
	sec := k["Secret"][0]
	data := pick(t, sec, "stringData").(map[string]any)
	if data["RUNGATED_API_KEY"] != "admin-key" || data["RUNGATED_DATABASE_URL"] == nil || data["OPENAI_API_KEY"] != "sk-test" {
		t.Errorf("secret keys: %v", data)
	}
	if _, ok := data["ANTHROPIC_API_KEY"]; ok {
		t.Error("unset secrets must not be rendered")
	}
	container := pick(t, dep, "spec", "template", "spec", "containers").([]any)[0].(map[string]any)
	envFrom := container["envFrom"].([]any)[0].(map[string]any)
	if pick(t, envFrom, "secretRef", "name") != "ab-rungated" {
		t.Errorf("envFrom %v", envFrom)
	}
	hpa := k["HorizontalPodAutoscaler"][0]
	if pick(t, hpa, "spec", "scaleTargetRef", "name") != pick(t, dep, "metadata", "name") {
		t.Error("hpa targets another object")
	}
	cfg, err := config.Load(writeConfig(t, out))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store.Kind != "postgres" || cfg.Bus.Kind != "nats" || cfg.Bus.URL != "nats://nats:4222" {
		t.Errorf("config %+v %+v", cfg.Store, cfg.Bus)
	}
}

func TestValidationFailures(t *testing.T) {
	cases := []struct{ name, values, want string }{
		{"replicas with embedded", "replicaCount: 2", "requires config.store.kind postgres"},
		{"autoscaling with embedded", "autoscaling: {enabled: true}", "autoscaling requires"},
		{"tier1 without onnx", "tier1: {enabled: true, modelsClaim: m}", "requires onnx.enabled"},
		{"tier1 without claim", "onnx: {enabled: true}\ntier1: {enabled: true}", "requires tier1.modelsClaim"},
		{"postgres without dsn", "config: {store: {kind: postgres}}", "requires secrets.databaseUrl"},
	}
	for _, c := range cases {
		_, err := render(t, c.values)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: expected error containing %q, got %v", c.name, c.want, err)
		}
	}
}

func TestTier1Render(t *testing.T) {
	out, err := render(t, `
onnx: {enabled: true}
tier1:
  enabled: true
  modelsClaim: rungated-models
  nerPath: /models/ner/gliner.int8.onnx
  nerTokenizerPath: /models/ner/tokenizer.json
  nerLabelsPath: /models/ner/labels.json
  temperature: 1.8
  threshold: 0.7
`)
	if err != nil {
		t.Fatal(err)
	}
	k := all(t, out)
	dep := k["Deployment"][0]
	spec := pick(t, dep, "spec", "template", "spec").(map[string]any)
	container := spec["containers"].([]any)[0].(map[string]any)
	if container["image"] != "ghcr.io/amareshhebbar/rungated:0.10.0-onnx" {
		t.Errorf("image %v", container["image"])
	}
	var hasMount, hasVolume bool
	for _, m := range container["volumeMounts"].([]any) {
		if m.(map[string]any)["name"] == "models" {
			hasMount = true
		}
	}
	for _, v := range spec["volumes"].([]any) {
		vm := v.(map[string]any)
		if vm["name"] == "models" && pick(t, vm, "persistentVolumeClaim", "claimName") == "rungated-models" {
			hasVolume = true
		}
	}
	if !hasMount || !hasVolume {
		t.Error("models volume not wired")
	}
	cfg, err := config.Load(writeConfig(t, out))
	if err != nil {
		t.Fatal(err)
	}
	tc := cfg.Tier1
	if !tc.Enabled || tc.NERPath != "/models/ner/gliner.int8.onnx" || tc.NERLabelsPath != "/models/ner/labels.json" || tc.Temperature != 1.8 || tc.Threshold != 0.7 || tc.MaxLen != 256 || tc.TimeoutMS != 40 {
		t.Errorf("tier1 config %+v", tc)
	}
}

func TestOptionalObjects(t *testing.T) {
	out, err := render(t, `
ingress:
  enabled: true
  className: nginx
  tls:
    - hosts: [rungated.example.com]
      secretName: tls
networkPolicy:
  enabled: true
  allowedPodLabels: {app: agent}
monitoring:
  serviceMonitor: {enabled: true, labels: {release: prom}}
manifests:
  includeExamples: true
  inline:
    extra.yaml: |
      apiVersion: rungated.dev/v0
      kind: Agent
      metadata: {id: extra, owner: a@b.c, purpose: test}
      spec:
        model: {allowed: ["mock-*"]}
`)
	if err != nil {
		t.Fatal(err)
	}
	k := all(t, out)
	for _, kind := range []string{"Ingress", "NetworkPolicy", "ServiceMonitor"} {
		if len(k[kind]) != 1 {
			t.Errorf("expected one %s", kind)
		}
	}
	ep := pick(t, k["ServiceMonitor"][0], "spec", "endpoints").([]any)[0].(map[string]any)
	if pick(t, ep, "authorization", "type") != "Bearer" || pick(t, ep, "authorization", "credentials", "key") != "RUNGATED_API_KEY" {
		t.Errorf("service monitor must authenticate to /metrics: %v", ep)
	}
	if pick(t, ep, "authorization", "credentials", "name") != "ab-rungated" {
		t.Errorf("service monitor secret name %v", pick(t, ep, "authorization", "credentials", "name"))
	}
	backend := pick(t, k["Ingress"][0], "spec", "rules").([]any)[0].(map[string]any)
	port := pick(t, backend, "http", "paths").([]any)[0].(map[string]any)
	if pick(t, port, "backend", "service", "port", "name") != "http" {
		t.Errorf("ingress backend %v", port)
	}
	var mdoc doc
	for _, d := range k["ConfigMap"] {
		if strings.HasSuffix(pick(t, d, "metadata", "name").(string), "-manifests") {
			mdoc = d
		}
	}
	data := pick(t, mdoc, "data").(map[string]any)
	for _, name := range []string{"finance-agent.yaml", "extra.yaml"} {
		body, ok := data[name].(string)
		if !ok {
			t.Fatalf("manifest %s missing from %v", name, data)
		}
		if _, err := manifest.Parse([]byte(body)); err != nil {
			t.Errorf("manifest %s does not validate: %v", name, err)
		}
	}
}

func TestChartMetadata(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(chartDir, "Chart.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"apiVersion", "name", "version", "appVersion", "description"} {
		if c[key] == nil {
			t.Errorf("Chart.yaml lacks %s", key)
		}
	}
	if c["apiVersion"] != "v2" {
		t.Errorf("apiVersion %v", c["apiVersion"])
	}
}
