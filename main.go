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
	"strings"

	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go/v2"

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

	workingDir := os.Getenv("INPUT_WORKING_DIRECTORY")
	if workingDir == "" {
		workingDir = "."
	}
	log.Infow("Running in", "path", workingDir)

	// get all the env vars that are prefixed with SECRET_
	secrets := make([]*livekit.AgentSecret, 0)
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "SECRET_") {
			// ignore the SECRET_LIST env var
			if env == "SECRET_LIST" {
				continue
			}

			secretParts := strings.SplitN(strings.TrimPrefix(env, "SECRET_"), "=", 2)
			secretName := secretParts[0]
			secretValue := secretParts[1]

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
		lkUrl = os.Getenv("LIVEKIT_URL")
		lkApiKey = os.Getenv("LIVEKIT_API_KEY")
		lkApiSecret = os.Getenv("LIVEKIT_API_SECRET")

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

			secretName := secretParts[0]
			secretValue := secretParts[1]
			log.Infow("Loading secret from SECRET_LIST", "secret", secretName)
			secrets = append(secrets, &livekit.AgentSecret{
				Name:  secretName,
				Value: []byte(secretValue),
			})
		}
	}

	client, err := lksdk.NewAgentClient(lkUrl, lkApiKey, lkApiSecret)
	if err != nil {
		log.Errorw("Failed to create agent client", err)
		os.Exit(1)
	}

	// get the subdomain from the lkUrl
	subdomain := strings.Split(lkUrl, ".")[0]

	if len(secrets) == 0 {
		log.Infow("No secrets loaded")
	}

	switch operation {
	case "create":
		createAgent(client, subdomain, secrets, workingDir)
	case "deploy":
		deployAgent(client, secrets, workingDir)
	case "status":
		agentStatus(client, workingDir)
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

func agentStatus(client *lksdk.AgentClient, workingDir string) {
	lkConfig, exists, err := LoadTOMLFile(workingDir, LiveKitTOMLFile)
	if err != nil {
		log.Errorw("Failed to load livekit.toml", err)
		os.Exit(1)
	}

	if !exists {
		log.Errorw("livekit.toml not found", nil)
		os.Exit(1)
	}

	res, err := client.ListAgents(context.Background(), &livekit.ListAgentsRequest{
		AgentId: lkConfig.Agent.ID,
	})
	if err != nil {
		log.Errorw("Failed to get agent", err)
		os.Exit(1)
	}

	if len(res.Agents) == 0 {
		log.Errorw("Agent not found", nil)
		os.Exit(1)
	}

	for _, agent := range res.Agents {
		for _, regionalAgent := range agent.AgentDeployments {
			if regionalAgent.Status != "Running" {
				log.Errorw("Agent not running", nil)
				sendSlackNotification(fmt.Sprintf("Agent %s is not running", lkConfig.Agent.ID))
				os.Exit(1)
			}
		}
	}

	log.Infow("Agent status", "agent", lkConfig.Agent.ID, "status", res.Agents[0].AgentDeployments[0].Status)
}

func deployAgent(client *lksdk.AgentClient, secrets []*livekit.AgentSecret, workingDir string) {
	lkConfig, exists, err := LoadTOMLFile(workingDir, LiveKitTOMLFile)
	if err != nil {
		log.Errorw("Failed to load livekit.toml", err)
		os.Exit(1)
	}

	if !exists {
		log.Errorw("livekit.toml not found", nil)
		os.Exit(1)
	}

	req := &livekit.DeployAgentRequest{
		AgentId: lkConfig.Agent.ID,
		Secrets: secrets,
	}

	resp, err := client.DeployAgent(context.Background(), req)
	if err != nil {
		log.Errorw("Failed to deploy agent", err)
		os.Exit(1)
	}

	err = UploadTarball(workingDir, resp.PresignedUrl, []string{LiveKitTOMLFile})
	if err != nil {
		log.Errorw("Failed to upload tarball", err)
		os.Exit(1)
	}

	err = Build(context.Background(), resp.AgentId, &ProjectConfig{
		URL:       lkUrl,
		APIKey:    lkApiKey,
		APISecret: lkApiSecret,
	})
	if err != nil {
		log.Errorw("Failed to build agent", err)
		os.Exit(1)
	}

	log.Infow("Agent deployed", "agent", resp.AgentId)
}

func createAgent(client *lksdk.AgentClient, subdomain string, secrets []*livekit.AgentSecret, workingDir string) {
	if _, err := os.Stat(fmt.Sprintf("%s/%s", workingDir, LiveKitTOMLFile)); err == nil {
		log.Infow("livekit.toml already exists", "path", fmt.Sprintf("%s/%s", workingDir, LiveKitTOMLFile))
		os.Exit(0)
	}

	lkConfig := NewLiveKitTOML(subdomain).WithDefaultAgent()

	req := &livekit.CreateAgentRequest{
		Secrets: secrets,
	}

	resp, err := client.CreateAgent(context.Background(), req)
	if err != nil {
		log.Errorw("Failed to create agent", err)
		os.Exit(1)
	}

	lkConfig.Agent.ID = resp.AgentId
	if err := lkConfig.SaveTOMLFile(workingDir, LiveKitTOMLFile); err != nil {
		log.Errorw("Failed to save livekit.toml", err)
		os.Exit(1)
	}

	err = UploadTarball(workingDir, resp.PresignedUrl, []string{LiveKitTOMLFile})
	if err != nil {
		log.Errorw("Failed to upload tarball", err)
		os.Exit(1)
	}

	err = Build(context.Background(), resp.AgentId, &ProjectConfig{
		URL:       lkUrl,
		APIKey:    lkApiKey,
		APISecret: lkApiSecret,
	})
	if err != nil {
		log.Errorw("Failed to build agent", err)
		os.Exit(1)
	}

	log.Infow("Agent created", "agent", resp.AgentId)
}
