package main

import (
	"context"
	"fmt"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main3() {
	ctx := context.Background()

	// Create a new client, with no features.
	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-client", Version: "v1.0.0"}, nil)

	// Connect to context7 MCP server over HTTP using streamable transport.
	transport := &mcp.StreamableClientTransport{
		Endpoint: "https://mcp.higress.ai/mcp-e2bdev/cmhoimriw0056bf01905mhbu3", // Context7 MCP endpoint
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()

	// List available tools first
	// tools, err := session.ListTools(ctx, nil)
	// if err != nil {
	// 	log.Fatalf("ListTools failed: %v", err)
	// }
	// log.Println("Available tools:")
	// for _, tool := range tools.Tools {
	// 	log.Printf("  - Tool: %s, Description: %s", tool.Name, tool.Description)
	// }
	// log.Println()

	//Call a tool on the server.
	// params := &mcp.CallToolParams{
	// 	Name:      "create_sandbox",
	// 	Arguments: map[string]any{"timeout": 500, "templateID": "code-interpreter-beta"}, // "base" is the default Python template
	// }
	params := &mcp.CallToolParams{
		Name:      "execute_code_sandbox",
		Arguments: map[string]any{"sandbox_id": "irqa3z0v07dft5n3ljfw4-6532622b", "code": "import math\nmath.sqrt(16)"}, // "base" is the default Python template
	}
	res, err := session.CallTool(ctx, params)
	if err != nil {
		log.Fatalf("CallTool failed: %v", err)
	}
	if res.IsError {
		// Print detailed error information
		errorMsg := "tool failed"
		if len(res.Content) > 0 {
			if textContent, ok := res.Content[0].(*mcp.TextContent); ok {
				errorMsg = textContent.Text
			} else {
				// Try to print the content as-is if it's not TextContent
				errorMsg = fmt.Sprintf("tool failed: %+v", res.Content)
			}
		}
		log.Fatalf("Tool error: %s", errorMsg)
	}
	for _, c := range res.Content {
		if textContent, ok := c.(*mcp.TextContent); ok {
			log.Print(textContent.Text)
		} else {
			log.Printf("Content: %+v", c)
		}
	}
}
