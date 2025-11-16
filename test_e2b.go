package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/xingyunyang/codeagents_go/pkg/agents"
	"github.com/xingyunyang/codeagents_go/pkg/executors"
	"github.com/xingyunyang/codeagents_go/pkg/models"
)

func main() {
	// API token
	token := "xxxxxxxxxxxxx"

	// 1. 创建E2B MCP执行器
	fmt.Println("🔧 创建E2B MCP执行器...")
	e2bExecutor, err := executors.NewE2BMCPExecutor(&executors.E2BMCPExecutorOptions{
		Endpoint:        "https://mcp.higress.ai/mcp-e2bdev/cmhoimxxxxxxxxxxxxf01905mhbu3",
		TemplateID:      "code-interpreter-beta", // ✨ E2B模板ID (base=Python环境)
		SandboxTimeout:  600,                     // 沙盒超时10分钟
		DefaultTimeout:  60 * time.Second,        // 代码执行超时60秒
		AutoKillSandbox: false,                   // 不自动清理，允许在多次执行间保持状态
	})
	if err != nil {
		log.Fatalf("❌ 创建E2B执行器失败: %v", err)
	}
	defer e2bExecutor.Close()

	// 2. 连接到E2B MCP服务器
	fmt.Println("📡 连接到E2B MCP服务器...")
	ctx := context.Background()
	if err := e2bExecutor.Connect(ctx); err != nil {
		log.Fatalf("❌ 连接失败: %v", err)
	}
	fmt.Println("✅ 已连接到E2B MCP服务器")

	// 3. 创建大模型
	fmt.Println("\n🤖 创建大模型...")
	model := models.NewOpenAIServerModel(
		"qwen-max",
		"https://dashscope.aliyuncs.com/compatible-mode/v1",
		token,
		map[string]interface{}{},
	)
	fmt.Println("✅ 模型创建成功")

	// 4. 创建ReactCodeAgent，并传入E2B执行器
	fmt.Println("\n🎯 创建ReactCodeAgent（使用E2B执行器）...")
	agentOptions := &agents.ReactCodeAgentOptions{
		PlanningInterval: 3,
		MaxSteps:         15,
		Verbose:          true,
		StreamOutputs:    true,
		CustomExecutor:   e2bExecutor, // ✨ 使用E2B执行器而不是默认的Go执行器
	}
	agent, err := agents.NewReactCodeAgent(model, nil, "", agentOptions)
	if err != nil {
		log.Fatalf("❌ 创建Agent失败: %v", err)
	}
	fmt.Println("✅ Agent创建成功（使用E2B Python沙盒）")
	defer agent.Close()

	// 5. 运行Agent - 现在它会在E2B沙盒中执行Python代码
	fmt.Println("\n🚀 启动Agent任务...")
	fmt.Println("📝 任务: 计算斐波那契数列的第10项")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	runCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	result, err := agent.Run(&agents.RunOptions{
		Task:    "计算斐波那契数列的第10项，并输出计算过程",
		Context: runCtx,
	})
	cancel()

	// 6. 显示结果
	fmt.Println("\n" + "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if err != nil {
		fmt.Printf("❌ 执行出错: %v\n", err)
		if result != nil {
			fmt.Printf("状态: %s\n", result.State)
			fmt.Printf("步数: %d\n", result.StepCount)
		}
	} else if result != nil {
		fmt.Printf("✅ 任务完成!\n")
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("状态: %s\n", result.State)
		fmt.Printf("步数: %d\n", result.StepCount)
		fmt.Printf("\n📝 最终答案:\n%s\n", result.Output)
	}

	fmt.Println("\n" + "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("💡 说明:")
	fmt.Println("   • Agent使用E2B沙盒执行Python代码")
	fmt.Println("   • 所有代码运行在隔离的远程环境中")
	fmt.Println("   • 支持Python标准库和常用第三方库")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}
