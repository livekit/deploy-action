// Copyright 2025 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"
	"github.com/livekit/server-sdk-go/v2/pkg/cloudagents"
)

const testURL = "wss://my-project.livekit.cloud"

func TestMain(m *testing.M) {
	log = logger.GetLogger()
	os.Exit(m.Run())
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tomlFor(subdomain, agentID string) string {
	return "[project]\n  subdomain = \"" + subdomain + "\"\n\n[agent]\n  id = \"" + agentID + "\"\n"
}

func TestResolveAgentTarget(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "livekit.toml", tomlFor("my-project", "CA_default"))
	writeFile(t, dir, "livekit.stage.toml", tomlFor("my-project", "CA_stage"))
	writeFile(t, dir, "stage/livekit.toml", tomlFor("my-project", "CA_stage_dir"))
	writeFile(t, dir, "livekit.other.toml", tomlFor("other-project", "CA_other"))
	writeFile(t, dir, "livekit.noagent.toml", "[project]\n  subdomain = \"my-project\"\n")
	writeFile(t, dir, "livekit.bad.toml", "this is = = not toml")

	empty := t.TempDir()

	tests := []struct {
		name       string
		agentID    string
		config     string
		dir        string
		wantID     string
		wantSource string
		wantConfig string
		wantErr    string
	}{
		{name: "default livekit.toml", dir: dir, wantID: "CA_default", wantSource: agentIDSourceTOML, wantConfig: "livekit.toml"},
		{name: "CONFIG file", config: "livekit.stage.toml", dir: dir, wantID: "CA_stage", wantSource: agentIDSourceConfig, wantConfig: "livekit.stage.toml"},
		{name: "CONFIG in subdirectory", config: "stage/livekit.toml", dir: dir, wantID: "CA_stage_dir", wantSource: agentIDSourceConfig, wantConfig: "stage/livekit.toml"},
		{name: "AGENT_ID beats CONFIG and toml", agentID: "CA_input", config: "livekit.stage.toml", dir: dir, wantID: "CA_input", wantSource: agentIDSourceInput},
		{name: "AGENT_ID needs no toml", agentID: " CA_input ", dir: empty, wantID: "CA_input", wantSource: agentIDSourceInput},
		{name: "AGENT_ID skips a mismatched toml", agentID: "CA_input", config: "livekit.other.toml", dir: dir, wantID: "CA_input", wantSource: agentIDSourceInput},
		{name: "missing default toml", dir: empty, wantErr: "livekit.toml not found"},
		{name: "missing CONFIG file", config: "livekit.prod.toml", dir: dir, wantErr: "livekit.prod.toml not found"},
		{name: "CONFIG subdomain mismatch", config: "livekit.other.toml", dir: dir, wantErr: "project does not match agent subdomain [other-project]"},
		{name: "toml without agent", config: "livekit.noagent.toml", dir: dir, wantErr: "no agent ID in livekit.noagent.toml"},
		{name: "malformed toml", config: "livekit.bad.toml", dir: dir, wantErr: "failed to load livekit.bad.toml"},
		{name: "absolute CONFIG", config: "/etc/livekit.toml", dir: dir, wantErr: "must be a path relative to WORKING_DIRECTORY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveAgentTarget(tt.agentID, tt.config, tt.dir, testURL)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.AgentID != tt.wantID || got.Source != tt.wantSource || got.ConfigFile != tt.wantConfig {
				t.Fatalf("got %+v, want id=%s source=%s config=%s", got, tt.wantID, tt.wantSource, tt.wantConfig)
			}
		})
	}
}

func TestCheckSubdomain(t *testing.T) {
	tests := []struct {
		name     string
		toml     string
		url      string
		wantErr  bool
		wantWarn bool
	}{
		{name: "match wss", toml: "my-project", url: "wss://my-project.livekit.cloud"},
		{name: "match https", toml: "my-project", url: "https://my-project.livekit.cloud"},
		{name: "match ignores case", toml: "My-Project", url: "wss://my-project.livekit.cloud"},
		{name: "mismatch", toml: "prod-project", url: "wss://stage-project.livekit.cloud", wantErr: true},
		{name: "empty toml subdomain", toml: "", url: "wss://my-project.livekit.cloud", wantWarn: true},
		{name: "unparseable url", toml: "my-project", url: "my-project.livekit.cloud", wantWarn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warn, err := checkSubdomain(tt.toml, tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if (warn != "") != tt.wantWarn {
				t.Fatalf("warning = %q, wantWarn %v", warn, tt.wantWarn)
			}
		})
	}
}

func TestValidateDeploymentName(t *testing.T) {
	valid := []string{"", "staging", "dev", "pr-123", "feature_x", "v1.2.3", "a", "A1", strings.Repeat("a", 63)}
	for _, name := range valid {
		if err := validateDeploymentName(name); err != nil {
			t.Errorf("validateDeploymentName(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"production", "Production", "-staging", "staging-", ".dev", "dev.", "_x", "x_", "has space", "feat/x", "näme", strings.Repeat("a", 64)}
	for _, name := range invalid {
		if err := validateDeploymentName(name); err == nil {
			t.Errorf("validateDeploymentName(%q) = nil, want error", name)
		}
	}
}

func TestParseAttributes(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    map[string]string
		wantErr bool
	}{
		{name: "empty", raw: "", want: nil},
		{name: "whitespace", raw: "  \n", want: nil},
		{name: "empty object", raw: "{}", want: nil},
		{name: "null", raw: "null", want: nil},
		{name: "strings", raw: `{"git_commit":"abc123","env":"staging"}`, want: map[string]string{"git_commit": "abc123", "env": "staging"}},
		{name: "multiline", raw: "{\n  \"a\": \"1\"\n}\n", want: map[string]string{"a": "1"}},
		{name: "number value", raw: `{"build": 42}`, wantErr: true},
		{name: "bool value", raw: `{"ok": true}`, wantErr: true},
		{name: "nested value", raw: `{"a": {"b": "c"}}`, wantErr: true},
		{name: "array", raw: `["a"]`, wantErr: true},
		{name: "key=value", raw: `git_commit=abc`, wantErr: true},
		{name: "empty key", raw: `{"": "x"}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAttributes(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func agentWith(id string, deployments ...*livekit.AgentDeployment) []*livekit.AgentInfo {
	return []*livekit.AgentInfo{{AgentId: id, AgentDeployments: deployments}}
}

func dep(region, deployment, status string) *livekit.AgentDeployment {
	return &livekit.AgentDeployment{Region: region, Deployment: deployment, Status: status, DeploymentEnabled: deployment != ""}
}

func TestEvaluateStatus(t *testing.T) {
	const id = "CA_test"
	tests := []struct {
		name       string
		agents     []*livekit.AgentInfo
		deployment string
		wantErr    string
	}{
		{name: "production running, no deployment field", agents: agentWith(id, dep("us-east", "", "Running"))},
		{name: "production named explicitly", agents: agentWith(id, dep("us-east", "production", "Running"))},
		{name: "all production regions running", agents: agentWith(id, dep("us-east", "", "Running"), dep("eu-central", "", "Running"))},
		{name: "one production region down", agents: agentWith(id, dep("us-east", "", "Running"), dep("eu-central", "", "CrashLoop")), wantErr: "is not running CrashLoop"},
		{name: "production sleeping is unhealthy", agents: agentWith(id, dep("us-east", "", "Sleeping")), wantErr: "is not running Sleeping"},
		{name: "sleeping staging ignored for production", agents: agentWith(id, dep("us-east", "", "Running"), dep("us-east", "staging", "Sleeping"))},
		{name: "broken staging ignored for production", agents: agentWith(id, dep("us-east", "", "Running"), dep("us-east", "staging", "Build Failed"))},
		{name: "staging running", agents: agentWith(id, dep("us-east", "", "Running"), dep("us-east", "staging", "Running")), deployment: "staging"},
		{name: "staging sleeping is healthy", agents: agentWith(id, dep("us-east", "", "Running"), dep("us-east", "staging", "Sleeping")), deployment: "staging"},
		{name: "staging waking is not yet healthy", agents: agentWith(id, dep("us-east", "staging", "Waking")), deployment: "staging", wantErr: "is not running Waking"},
		{name: "staging building", agents: agentWith(id, dep("us-east", "staging", "Building")), deployment: "staging", wantErr: "is not running Building"},
		{name: "production down ignored for staging", agents: agentWith(id, dep("us-east", "", "Error"), dep("us-east", "staging", "Running")), deployment: "staging"},
		{name: "deployment missing", agents: agentWith(id, dep("us-east", "", "Running")), deployment: "staging", wantErr: "has no staging deployment"},
		{name: "no deployments at all", agents: agentWith(id), wantErr: "has no production deployment"},
		{name: "other agents ignored", agents: append(agentWith("CA_other", dep("us-east", "", "Error")), agentWith(id, dep("us-east", "", "Running"))...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := evaluateStatus(tt.agents, id, tt.deployment)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseAgentIDs(t *testing.T) {
	if got := parseAgentIDs(""); len(got) != 0 {
		t.Fatalf("parseAgentIDs(\"\") = %v, want empty", got)
	}
	want := []string{"CA_a", "CA_b"}
	if got := parseAgentIDs(" CA_a, ,CA_b ,"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestBuildExcludes runs the excludes through the SDK's tarball builder to check
// which files are actually uploaded.
func TestBuildExcludes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"agent.py",
		"Dockerfile",
		"livekit.toml",
		"livekit.stage.toml",
		"stage/livekit.toml",
		"envs/prod.toml",
		"pyproject.toml",
	} {
		writeFile(t, dir, name, "x")
	}

	tests := []struct {
		name       string
		configFile string
		wantFiles  []string
	}{
		{name: "default", configFile: "livekit.toml", wantFiles: []string{"Dockerfile", "agent.py", "envs/prod.toml", "pyproject.toml"}},
		{name: "AGENT_ID, no toml", configFile: "", wantFiles: []string{"Dockerfile", "agent.py", "envs/prod.toml", "pyproject.toml"}},
		{name: "custom CONFIG name", configFile: "envs/prod.toml", wantFiles: []string{"Dockerfile", "agent.py", "pyproject.toml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := cloudagents.CreateSourceTarball(os.DirFS(dir), buildExcludes(tt.configFile), &buf); err != nil {
				t.Fatal(err)
			}
			got := tarFiles(t, &buf)
			if !reflect.DeepEqual(got, tt.wantFiles) {
				t.Fatalf("uploaded %v, want %v", got, tt.wantFiles)
			}
		})
	}
}

func tarFiles(t *testing.T, r io.Reader) []string {
	t.Helper()
	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var files []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			files = append(files, h.Name)
		}
	}
	slices.Sort(files)
	return files
}
