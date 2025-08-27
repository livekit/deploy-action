package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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

			secretParts := strings.Split(strings.TrimPrefix(env, "SECRET_"), "=")
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
			secretParts := strings.Split(secret, "=")
			secretName := secretParts[0]
			secretValue := secretParts[1]
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

	fmt.Println("Hello, World!")
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

	cmd := exec.Command("git", "config", "user.name", "github-actions[bot]")
	cmd.Dir = workingDir
	if err := cmd.Run(); err != nil {
		log.Errorw("Error configuring user.name", err)
		os.Exit(1)
	}

	cmd = exec.Command("git", "config", "user.email", "github-actions[bot]@users.noreply.github.com")
	cmd.Dir = workingDir
	if err := cmd.Run(); err != nil {
		log.Errorw("Error configuring user.email", err)
		os.Exit(1)
	}

	cmd = exec.Command("git", "add", fmt.Sprintf("%s/%s", workingDir, LiveKitTOMLFile))
	if err := cmd.Run(); err != nil {
		log.Errorw("Error adding file to git", err)
		os.Exit(1)
	}

	cmd = exec.Command("git", "commit", "-m", fmt.Sprintf("Add livekit.toml agent config for %s in %s", resp.AgentId, workingDir))
	if err := cmd.Run(); err != nil {
		log.Errorw("Error committing file to git", err)
		os.Exit(1)
	}

	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken != "" {
		// Get the current remote URL and modify it to include the token
		cmd = exec.Command("git", "remote", "get-url", "origin")
		cmd.Dir = workingDir
		output, err := cmd.Output()
		if err == nil {
			remoteURL := strings.TrimSpace(string(output))
			// Replace https://github.com with https://token@github.com
			authenticatedURL := strings.Replace(remoteURL, "https://github.com", "https://"+githubToken+"@github.com", 1)
			cmd = exec.Command("git", "remote", "set-url", "origin", authenticatedURL)
			cmd.Dir = workingDir
			if err := cmd.Run(); err != nil {
				log.Errorw("Error setting git remote URL", err)
				os.Exit(1)
			}
		}
	}

	cmd = exec.Command("git", "push")
	if err := cmd.Run(); err != nil {
		log.Errorw("Error pushing file to git", err)
		os.Exit(1)
	}

	log.Infow("livekit.toml agent config committed", "agent", resp.AgentId)
}
