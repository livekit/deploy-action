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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"
	"github.com/livekit/server-sdk-go/v2/pkg/cloudagents"

	"github.com/slack-go/slack"
)

var (
	log                          logger.Logger
	lkUrl, lkApiKey, lkApiSecret string
)

func main() {
	zl, _ := logger.NewZapLogger(&logger.Config{
		JSON:  true,
		Level: "debug",
	})
	log = zl.WithValues()
	logger.SetLogger(log, "cloud-agents-github-plugin")

	operation := os.Getenv("INPUT_OPERATION")
	if operation == "" {
		log.Errorw("OPERATION is not set", nil)
		os.Exit(1)
	}

	region := os.Getenv("INPUT_REGION")
	if region == "" {
		log.Infow("REGION is not set, defaulting to nearest region.")
	}

	workingDir := os.Getenv("INPUT_WORKING_DIRECTORY")
	if workingDir == "" {
		workingDir = "."
	}
	log.Infow("Running in", "path", workingDir)

	agentIds := parseAgentIDs(os.Getenv("INPUT_AGENT_IDS"))
	if len(agentIds) > 0 {
		log.Infow("Using agent IDs from INPUT_AGENT_IDS", "agentIds", agentIds)
	}

	agentIDInput := strings.TrimSpace(os.Getenv("INPUT_AGENT_ID"))
	configInput := strings.TrimSpace(os.Getenv("INPUT_CONFIG"))

	deployment := strings.TrimSpace(os.Getenv("INPUT_DEPLOYMENT"))
	if err := validateDeploymentName(deployment); err != nil {
		log.Errorw("Invalid DEPLOYMENT", err)
		os.Exit(1)
	}
	if deployment != "" {
		log.Infow("Targeting deployment", "deployment", deployment)
	}

	attributes, err := parseAttributes(os.Getenv("INPUT_ATTRIBUTES"))
	if err != nil {
		log.Errorw("Invalid ATTRIBUTES", err)
		os.Exit(1)
	}

	timeout := os.Getenv("INPUT_TIMEOUT")
	if timeout == "" {
		timeout = "5m"
	}
	timeoutDuration, err := time.ParseDuration(timeout)
	if err != nil {
		log.Errorw("Invalid timeout", err)
		os.Exit(1)
	}

	// get all the env vars that are prefixed with SECRET_
	secrets := make([]*livekit.AgentSecret, 0)
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "SECRET_") {
			// ignore the SECRET_LIST env var
			if strings.HasPrefix(env, "SECRET_LIST=") {
				continue
			}

			secretParts := strings.SplitN(strings.TrimPrefix(env, "SECRET_"), "=", 2)
			secretName := secretParts[0]
			secretValue := strings.TrimSpace(secretParts[1])

			log.Infow("Loading secret", "secret", secretName)
			if secretName == "LIVEKIT_URL" || secretName == "LIVEKIT_API_KEY" || secretName == "LIVEKIT_API_SECRET" {
				switch secretName {
				case "LIVEKIT_URL":
					lkUrl = secretValue
				case "LIVEKIT_API_KEY":
					lkApiKey = secretValue
				case "LIVEKIT_API_SECRET":
					lkApiSecret = secretValue
				}
			}
			secrets = append(secrets, &livekit.AgentSecret{
				Name:  secretName,
				Value: []byte(secretValue),
			})
		}
	}

	if lkUrl == "" || lkApiKey == "" || lkApiSecret == "" {
		// try to load directly from the env first instead of the SECRET_ prefix
		lkUrl = strings.TrimSpace(os.Getenv("LIVEKIT_URL"))
		lkApiKey = strings.TrimSpace(os.Getenv("LIVEKIT_API_KEY"))
		lkApiSecret = strings.TrimSpace(os.Getenv("LIVEKIT_API_SECRET"))

		if lkUrl == "" || lkApiKey == "" || lkApiSecret == "" {
			log.Errorw("LIVEKIT_URL, LIVEKIT_API_KEY, and LIVEKIT_API_SECRET must be set", nil)
			os.Exit(1)
		}
	}

	// some use cases require a list of secrets to be passed in as a comma separated list of SECRET_NAME=SECRET_VALUE
	if os.Getenv("SECRET_LIST") != "" {
		secretList := strings.Split(os.Getenv("SECRET_LIST"), ",")
		for _, secret := range secretList {
			secretParts := strings.SplitN(secret, "=", 2)
			if len(secretParts) != 2 {
				log.Errorw("Invalid secret format", nil, "secret", secret)
				os.Exit(1)
			}

			secretName := strings.TrimSpace(secretParts[0])
			secretValue := strings.TrimSpace(secretParts[1])
			log.Infow("Loading secret from SECRET_LIST", "secret", secretName)
			secrets = append(secrets, &livekit.AgentSecret{
				Name:  secretName,
				Value: []byte(secretValue),
			})
		}
	}

	client, err := cloudagents.New(
		cloudagents.WithProject(lkUrl, lkApiKey, lkApiSecret),
		cloudagents.WithLogger(log),
	)
	if err != nil {
		log.Errorw("Failed to create agent client", err)
		os.Exit(1)
	}

	// get the subdomain from the lkUrl
	subdomain := strings.Split(lkUrl, ".")[0]

	if len(secrets) == 0 {
		log.Infow("No secrets loaded")
	}

	// resolve the agent an operation acts on: AGENT_ID > CONFIG > livekit.toml
	resolveTarget := func() *agentTarget {
		target, err := resolveAgentTarget(agentIDInput, configInput, workingDir, lkUrl)
		if err != nil {
			log.Errorw("Failed to resolve agent", err)
			os.Exit(1)
		}
		log.Infow("Using agent", "agent", target.AgentID, "source", target.Source, "config", target.ConfigFile)
		return target
	}

	switch operation {
	case "create":
		if agentIDInput != "" {
			log.Errorw("AGENT_ID cannot be used with create; create registers a new agent", nil)
			os.Exit(1)
		}
		if deployment != "" {
			log.Errorw("DEPLOYMENT cannot be used with create; create the agent first, then deploy with DEPLOYMENT", nil)
			os.Exit(1)
		}
		configFile, err := configFileName(configInput)
		if err != nil {
			log.Errorw("Invalid CONFIG", err)
			os.Exit(1)
		}
		createAgent(client, subdomain, secrets, workingDir, configFile, region, attributes)
	case "deploy":
		if deployment != "" && len(secrets) > 0 {
			log.Warnw("Secrets are shared by every deployment of an agent; updating them for this deployment also changes what production reads", nil, "deployment", deployment)
		}
		deployAgent(client, secrets, workingDir, resolveTarget(), deployment, attributes)
	case "promote":
		if deployment == "" {
			log.Errorw("promote requires DEPLOYMENT, the non-production deployment to promote to production", nil)
			os.Exit(1)
		}
		promoteAgent(client, resolveTarget(), deployment)
	case "status":
		err := agentStatus(client, resolveTarget(), deployment)
		if err != nil {
			log.Errorw("Failed to get agent status", err)
			os.Exit(1)
		}
	case "status-retry":
		target := resolveTarget()
		log.Debugw("Starting agent status retry", "timeout", timeoutDuration)
		err := agentStatusRetry(client, target, deployment, timeoutDuration)
		if err != nil {
			log.Errorw("Failed to get agent status", err)
			os.Exit(1)
		}
		log.Infow("Agent status check completed", "status", "running")
	case "delete":
		deleteAgent(client, resolveTarget(), deployment)
	case "delete-multi":
		if deployment != "" {
			log.Errorw("DEPLOYMENT cannot be used with delete-multi", nil)
			os.Exit(1)
		}
		if len(agentIds) == 0 {
			log.Errorw("delete-multi requires AGENT_IDS", nil)
			os.Exit(1)
		}
		deleteAgentMulti(client, agentIds)
	default:
		log.Errorw("Invalid operation", nil, "operation", operation)
		os.Exit(1)
	}
}

func sendSlackNotification(message string) {
	slackToken := os.Getenv("SLACK_TOKEN")
	slackChannel := os.Getenv("SLACK_CHANNEL")

	if slackToken == "" || slackChannel == "" {
		log.Infow("Slack notification skipped - token or channel not configured")
		return
	}

	api := slack.New(slackToken)
	_, _, err := api.PostMessage(
		slackChannel,
		slack.MsgOptionText(message, false),
	)

	if err != nil {
		log.Errorw("Failed to send Slack notification", err)
	} else {
		log.Infow("Slack notification sent", "channel", slackChannel)
	}
}

func agentStatusRetry(client *cloudagents.Client, target *agentTarget, deployment string, timeoutDuration time.Duration) error {
	startTime := time.Now()
	for {
		err := agentStatus(client, target, deployment)
		if err == nil {
			return nil
		}

		if time.Since(startTime) >= timeoutDuration {
			return fmt.Errorf("timeout reached after %v", timeoutDuration)
		}

		log.Infow("Failed to get running agent", "error", err)
		time.Sleep(5 * time.Second)
	}
}

func agentStatus(client *cloudagents.Client, target *agentTarget, deployment string) error {
	log.Infow("Getting agent status", "agent", target.AgentID, "deployment", deploymentLabel(deployment))

	res, err := client.ListAgents(context.Background(), &livekit.ListAgentsRequest{
		AgentId: target.AgentID,
	})
	if err != nil {
		return fmt.Errorf("failed to get agent")
	}

	if len(res.Agents) == 0 {
		return fmt.Errorf("agent not found")
	}

	regionalAgent, err := evaluateStatus(res.Agents, target.AgentID, deployment)
	if err != nil {
		if regionalAgent != nil {
			sendSlackNotification(fmt.Sprintf("Agent %s is not running", target.AgentID))
		}
		return err
	}

	log.Infow("Agent status", "agent", target.AgentID, "deployment", deploymentLabel(deployment), "status", regionalAgent.Status)
	return nil
}

func deployAgent(client *cloudagents.Client, secrets []*livekit.AgentSecret, workingDir string, target *agentTarget, deployment string, attributes map[string]string) {
	excludes := buildExcludes(target.ConfigFile)

	var err error
	if deployment == "" {
		err = client.DeployAgent(
			context.Background(),
			target.AgentID,
			os.DirFS(workingDir),
			secrets,
			attributes,
			excludes,
			os.Stderr,
		)
	} else {
		err = client.DeployAgentV2(
			context.Background(),
			target.AgentID,
			os.DirFS(workingDir),
			secrets,
			attributes,
			deployment,
			excludes,
			os.Stderr,
		)
	}
	if err != nil {
		log.Errorw("Failed to deploy agent", err)
		os.Exit(1)
	}

	log.Infow("Agent deployed", "agent", target.AgentID, "deployment", deploymentLabel(deployment))
}

func promoteAgent(client *cloudagents.Client, target *agentTarget, deployment string) {
	if err := client.PromoteAgent(context.Background(), target.AgentID, deployment, ""); err != nil {
		log.Errorw("Failed to promote agent", err)
		os.Exit(1)
	}

	log.Infow("Agent promoted to production", "agent", target.AgentID, "from", deployment)
}

func createAgent(client *cloudagents.Client, subdomain string, secrets []*livekit.AgentSecret, workingDir string, configFile string, region string, attributes map[string]string) {
	if _, err := os.Stat(filepath.Join(workingDir, configFile)); err == nil {
		log.Infow(fmt.Sprintf("%s already exists", configFile), "path", filepath.Join(workingDir, configFile))
		os.Exit(0)
	}
	// make sure the toml can be written before registering an agent
	if err := os.MkdirAll(filepath.Dir(filepath.Join(workingDir, configFile)), 0o755); err != nil {
		log.Errorw(fmt.Sprintf("Failed to create the directory for %s", configFile), err)
		os.Exit(1)
	}
	lkConfig := NewLiveKitTOML(subdomain).WithDefaultAgent()
	var regions []string
	if region != "" {
		regions = []string{region}
	}
	resp, err := client.CreateAgent(
		context.Background(),
		os.DirFS(workingDir),
		secrets,
		regions,
		attributes,
		buildExcludes(configFile),
		os.Stderr,
	)
	if err != nil {
		log.Errorw("Failed to create agent", err)
		os.Exit(1)
	}

	lkConfig.Agent.ID = resp.AgentId
	if err := lkConfig.SaveTOMLFile(workingDir, configFile); err != nil {
		log.Errorw(fmt.Sprintf("Failed to save %s", configFile), err)
		os.Exit(1)
	}

	log.Infow("Agent created", "agent", resp.AgentId)
}

func deleteAgent(client *cloudagents.Client, target *agentTarget, deployment string) {
	req := &livekit.DeleteAgentRequest{
		AgentId:    target.AgentID,
		Deployment: deployment,
	}

	res, err := client.DeleteAgent(context.Background(), req)
	if err != nil {
		log.Errorw("Failed to delete agent", err)
		os.Exit(1)
	}
	if !res.Success {
		log.Errorw("Failed to delete agent", nil, "message", res.Message)
		os.Exit(1)
	}

	if deployment != "" {
		log.Infow("Agent deployment deleted", "agent", target.AgentID, "deployment", deployment)
		return
	}
	log.Infow("Agent deleted", "agent", target.AgentID)
}

func deleteAgentMulti(client *cloudagents.Client, agentIds []string) {
	for _, agentId := range agentIds {
		req := &livekit.DeleteAgentRequest{
			AgentId: agentId,
		}

		res, err := client.DeleteAgent(context.Background(), req)
		if err != nil {
			log.Errorw("Failed to delete agent", err)
			os.Exit(1)
		}
		if !res.Success {
			log.Errorw("Failed to delete agent", nil, "agent", agentId, "message", res.Message)
			os.Exit(1)
		}

		log.Infow("Agent deleted", "agent", agentId)
	}
}
