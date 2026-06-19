package mcpserver

import "github.com/modelcontextprotocol/go-sdk/mcp"

func markToolReadOnly(tool *mcp.Tool, title string) {
	if tool == nil {
		return
	}
	destructive := false
	if tool.Annotations == nil {
		tool.Annotations = &mcp.ToolAnnotations{}
	}
	tool.Annotations.ReadOnlyHint = true
	tool.Annotations.DestructiveHint = &destructive
	if tool.Annotations.Title == "" {
		tool.Annotations.Title = title
	}
}

func markToolMutating(tool *mcp.Tool, title string) {
	if tool == nil {
		return
	}
	destructive := false
	if tool.Annotations == nil {
		tool.Annotations = &mcp.ToolAnnotations{}
	}
	tool.Annotations.ReadOnlyHint = false
	tool.Annotations.DestructiveHint = &destructive
	if tool.Annotations.Title == "" {
		tool.Annotations.Title = title
	}
}
