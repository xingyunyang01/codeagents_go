package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/xingyunyang/codeagents_go/pkg/agents"
	"github.com/xingyunyang/codeagents_go/pkg/models"
)

func main2() {
	// Get HuggingFace API token from environment
	token := "fc728f70f8a14c20xxxxxxxxxxxxxx.KhfL593adLdYNMFE"

	// Create model with explicit provider
	fmt.Println("Creating model with token:", token[:10]+"...")
	model := models.NewOpenAIServerModel(
		"glm-4.5-flash",
		"https://open.bigmodel.cn/api/paas/v4", // 基础URL
		token,
		map[string]interface{}{},
	)
	fmt.Println("Model created successfully")

	// Create ReactCodeAgent with verbose mode enabled
	fmt.Println("Creating ReactCodeAgent...")
	agentOptions := &agents.ReactCodeAgentOptions{
		PlanningInterval: 3,
		MaxSteps:         15,
		Verbose:          true, // 启用详细日志
		StreamOutputs:    true,
	}
	agent, err := agents.NewReactCodeAgent(model, nil, "", agentOptions)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	fmt.Println("Agent created successfully")
	defer agent.Close()

	// Run the agent with a longer timeout
	fmt.Println("Starting agent run...")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	result, err := agent.Run(&agents.RunOptions{
		Task:    "What is 5 factorial?",
		Context: ctx,
	})
	cancel()
	fmt.Println("Agent run completed")

	if err != nil {
		fmt.Printf("\nError: %v\n", err)
		if result != nil {
			fmt.Printf("Status: %s\n", result.State)
			fmt.Printf("Steps taken: %d\n", result.StepCount)
		}
	} else if result != nil {
		// 打印成功情况下的结果
		fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("✓ Task completed successfully!\n")
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("Status: %s\n", result.State)
		fmt.Printf("Steps taken: %d\n", result.StepCount)
		fmt.Printf("\n📝 Final Answer:\n%s\n", result.Output)
	}
}
