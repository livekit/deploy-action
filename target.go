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
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/livekit/protocol/livekit"
)

const (
	productionDeployment = "production"

	// Status strings reported by LiveKit Cloud in AgentDeployment.Status. They are
	// plain strings in the protocol; the values are documented at
	// https://docs.livekit.io/reference/developer-tools/livekit-cli/agent/deployment/manage/#status-values
	statusRunning  = "Running"
	statusSleeping = "Sleeping"
)

// Agent ID sources, in order of precedence.
const (
	agentIDSourceInput  = "AGENT_ID input"
	agentIDSourceConfig = "CONFIG file"
	agentIDSourceTOML   = "default livekit.toml"
)

// Same rules as LiveKit Cloud: start and end with an alphanumeric character,
// only alphanumerics, '-', '_' and '.', at most 63 characters.
// https://docs.livekit.io/deploy/agents/deployments/#deployment-names
var deploymentNamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,61}[A-Za-z0-9])?$`)

// agentTarget is the agent an operation acts on and where its ID came from.
type agentTarget struct {
	AgentID string
	Source  string
	// ConfigFile is the toml the ID was read from, relative to the working
	// directory. Empty when the ID came from AGENT_ID.
	ConfigFile string
}

// validateDeploymentName checks a non-production deployment name. An empty name
// targets production and is always valid.
func validateDeploymentName(name string) error {
	if name == "" {
		return nil
	}
	if strings.EqualFold(name, productionDeployment) {
		return fmt.Errorf("DEPLOYMENT %q is reserved; leave DEPLOYMENT empty to target production", name)
	}
	if !deploymentNamePattern.MatchString(name) {
		return fmt.Errorf("invalid DEPLOYMENT %q: must start and end with an alphanumeric character, contain only alphanumerics, '-', '_' and '.', and be at most 63 characters", name)
	}
	return nil
}

// parseAttributes parses the ATTRIBUTES input, a JSON object of string values.
// An empty input returns nil.
func parseAttributes(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var attrs map[string]string
	if err := json.Unmarshal([]byte(raw), &attrs); err != nil {
		return nil, fmt.Errorf("ATTRIBUTES must be a JSON object of string values: %w", err)
	}
	for k := range attrs {
		if strings.TrimSpace(k) == "" {
			return nil, errors.New("ATTRIBUTES must not contain an empty key")
		}
	}
	if len(attrs) == 0 {
		return nil, nil
	}
	return attrs, nil
}

// configFileName returns the toml path relative to the working directory: CONFIG
// when set, otherwise livekit.toml.
func configFileName(configInput string) (string, error) {
	if configInput == "" {
		return LiveKitTOMLFile, nil
	}
	if filepath.IsAbs(configInput) {
		return "", fmt.Errorf("CONFIG %q must be a path relative to WORKING_DIRECTORY", configInput)
	}
	return filepath.Clean(configInput), nil
}

// checkSubdomain fails when a toml's project subdomain doesn't match the project
// in LIVEKIT_URL, so a toml written for one project can't deploy into another.
// It returns a non-empty warning instead when either side can't be determined.
func checkSubdomain(tomlSubdomain, lkURL string) (warning string, err error) {
	urlSubdomain := ExtractSubdomain(lkURL)
	if tomlSubdomain == "" || urlSubdomain == "" {
		return "could not compare the toml project subdomain with LIVEKIT_URL, skipping the check", nil
	}
	if !strings.EqualFold(tomlSubdomain, urlSubdomain) {
		return "", fmt.Errorf("project does not match agent subdomain [%s]: LIVEKIT_URL is for project [%s]", tomlSubdomain, urlSubdomain)
	}
	return "", nil
}

// resolveAgentTarget picks the agent ID with precedence AGENT_ID > CONFIG >
// livekit.toml. When the ID comes from a toml, the toml's project must match
// LIVEKIT_URL.
func resolveAgentTarget(agentIDInput, configInput, workingDir, lkURL string) (*agentTarget, error) {
	if id := strings.TrimSpace(agentIDInput); id != "" {
		if configInput != "" {
			log.Warnw("AGENT_ID takes precedence, ignoring CONFIG", nil, "config", configInput)
		}
		return &agentTarget{AgentID: id, Source: agentIDSourceInput}, nil
	}

	fileName, err := configFileName(configInput)
	if err != nil {
		return nil, err
	}
	source := agentIDSourceTOML
	if configInput != "" {
		source = agentIDSourceConfig
	}

	lkConfig, exists, err := LoadTOMLFile(workingDir, fileName)
	if !exists {
		return nil, fmt.Errorf("%s not found in %s", fileName, workingDir)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load %s: %w", fileName, err)
	}
	if lkConfig.Agent == nil || lkConfig.Agent.ID == "" {
		return nil, fmt.Errorf("no agent ID in %s", fileName)
	}

	warning, err := checkSubdomain(lkConfig.Project.Subdomain, lkURL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fileName, err)
	}
	if warning != "" {
		log.Warnw(warning, nil, "config", fileName)
	}

	return &agentTarget{AgentID: lkConfig.Agent.ID, Source: source, ConfigFile: fileName}, nil
}

// buildExcludes lists the files kept out of the uploaded build context: every
// livekit*.toml, plus the toml in use when it has another name.
func buildExcludes(configFile string) []string {
	excludes := []string{LiveKitTOMLFile, "livekit*.toml", "**/livekit*.toml"}
	if configFile != "" && configFile != LiveKitTOMLFile && !strings.HasPrefix(configFile, "..") {
		excludes = append(excludes, filepath.ToSlash(configFile))
	}
	return excludes
}

func isProductionDeployment(name string) bool {
	return name == "" || name == productionDeployment
}

func deploymentLabel(name string) string {
	if isProductionDeployment(name) {
		return productionDeployment
	}
	return name
}

// filterDeployments returns the regional deployments of agentID that belong to
// the targeted deployment. An empty deployment targets production.
func filterDeployments(agents []*livekit.AgentInfo, agentID, deployment string) []*livekit.AgentDeployment {
	var matched []*livekit.AgentDeployment
	for _, agent := range agents {
		if agent.AgentId != "" && agent.AgentId != agentID {
			continue
		}
		for _, d := range agent.AgentDeployments {
			if isProductionDeployment(deployment) {
				if isProductionDeployment(d.Deployment) {
					matched = append(matched, d)
				}
			} else if d.Deployment == deployment {
				matched = append(matched, d)
			}
		}
	}
	return matched
}

// isHealthy reports whether a regional deployment counts as up. Non-production
// deployments sleep when idle and wake on demand, so Sleeping is healthy there.
func isHealthy(d *livekit.AgentDeployment, deployment string) bool {
	if d.Status == statusRunning {
		return true
	}
	return !isProductionDeployment(deployment) && d.Status == statusSleeping
}

// evaluateStatus checks every region of the targeted deployment and returns the
// first one that isn't healthy.
func evaluateStatus(agents []*livekit.AgentInfo, agentID, deployment string) (*livekit.AgentDeployment, error) {
	matched := filterDeployments(agents, agentID, deployment)
	if len(matched) == 0 {
		return nil, fmt.Errorf("agent id %s has no %s deployment", agentID, deploymentLabel(deployment))
	}
	for _, d := range matched {
		if !isHealthy(d, deployment) {
			return d, fmt.Errorf("agent id %s is not running %s", agentID, d.Status)
		}
	}
	return matched[0], nil
}

// parseAgentIDs splits the comma-separated AGENT_IDS input, dropping blanks.
func parseAgentIDs(raw string) []string {
	var ids []string
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
