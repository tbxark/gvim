package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultEnvDetectorsMatchFresh(t *testing.T) {
	dets := DefaultEnvDetectors()
	if len(dets) != 6 {
		t.Fatalf("detectors = %d, want 6", len(dets))
	}
	wantNames := []string{".venv", "venv", "direnv", "mise", "pipenv", "poetry"}
	for i, name := range wantNames {
		if dets[i].Name != name {
			t.Errorf("detector %d name = %q, want %q", i, dets[i].Name, name)
		}
	}
	if dets[0].Kind != EnvKindPathOnly || dets[1].Kind != EnvKindPathOnly {
		t.Errorf("venv detectors = %v, want path-only", dets[:2])
	}
	for _, d := range dets[2:] {
		if d.Kind != EnvKindShell {
			t.Errorf("%s kind = %q, want shell", d.Name, d.Kind)
		}
	}
	if len(dets[0].Require) == 0 || len(dets[1].Require) == 0 {
		t.Error("the virtualenv detectors need interpreter evidence")
	}
	if errs := ValidateEnvDetectors(dets); len(errs) != 0 {
		t.Errorf("built-ins do not validate: %v", errs)
	}
}

func TestEnvDetectorsUserConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"env":{"detectors":[
	  {"name":"conda","markers":["environment.yml"],"kind":"shell","snippet":"eval \"$(conda shell.bash hook)\" && conda activate ."}
	]}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, diags, err := Load(Options{UserFile: path})
	if err != nil || len(diags) != 0 {
		t.Fatalf("load = %v %v", err, diags)
	}
	dets := cfg.EnvDetectors()
	if len(dets) != 1 || dets[0].Name != "conda" || dets[0].Kind != EnvKindShell {
		t.Fatalf("detectors = %+v", dets)
	}
}

func TestEnvDetectorsProjectLayerRefused(t *testing.T) {
	user := t.TempDir()
	userFile := filepath.Join(user, "config.json")
	if err := os.WriteFile(userFile, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	project := filepath.Join(root, ".gvim", "config.json")
	if err := os.MkdirAll(filepath.Dir(project), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"env":{"detectors":[{"name":"evil","markers":[".git"],"kind":"shell","snippet":"rm -rf /"}]}}`
	if err := os.WriteFile(project, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, diags, err := Load(Options{Root: root, UserFile: userFile})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range diags {
		if d.Key == "env" && strings.Contains(d.Message, "project config cannot set env") {
			found = true
		}
	}
	if !found {
		t.Errorf("no project env diagnostic: %v", diags)
	}
	if len(cfg.EnvDetectors()) != len(DefaultEnvDetectors()) {
		t.Errorf("project env changed the detectors: %+v", cfg.EnvDetectors())
	}
}

func TestEnvDetectorValidation(t *testing.T) {
	bad := []EnvDetector{
		{Name: "", Markers: []string{".venv"}, Kind: EnvKindPathOnly, Snippet: "x"},
		{Name: "a/b", Markers: []string{"../escape"}, Kind: EnvKindPathOnly, Snippet: "x"},
		{Name: "nokind", Markers: []string{".envrc"}, Snippet: "x"},
		{Name: "nosnippet", Markers: []string{"Pipfile"}, Kind: EnvKindShell},
	}
	if got := ValidateEnvDetectors(bad); len(got) < 4 {
		t.Errorf("validation problems = %v, want one per bad detector", got)
	}
}
