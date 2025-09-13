package contract

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ca-srg/ragent/internal/types"
	"github.com/google/jsonschema-go/jsonschema"
)

// TestMCPProtocolCompliance validates that all MCP types comply with the protocol specification
func TestMCPProtocolCompliance(t *testing.T) {
	t.Run("JSON-RPC 2.0 Base Structure", testJSONRPCBaseStructure)
	t.Run("Tool Definition Schema", testToolDefinitionSchema)
	t.Run("Request Message Format", testRequestMessageFormat)
	t.Run("Response Message Format", testResponseMessageFormat)
	t.Run("Error Format", testErrorFormat)
	t.Run("Tool List Response", testToolListResponse)
	t.Run("Tool Call Response", testToolCallResponse)
}

func testJSONRPCBaseStructure(t *testing.T) {
	tests := []struct {
		name     string
		message  interface{}
		expected map[string]interface{}
	}{
		{
			name: "MCPToolRequest has required JSON-RPC fields",
			message: types.LegacyMCPToolRequest{
				JSONRPC: "2.0",
				ID:      "test-1",
				Method:  "tools/list",
				Params:  nil,
			},
			expected: map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      "test-1",
				"method":  "tools/list",
			},
		},
		{
			name: "MCPToolResponse has required JSON-RPC fields",
			message: types.MCPToolResponse{
				JSONRPC: "2.0",
				ID:      "test-1",
				Result:  map[string]interface{}{"test": "result"},
				Error:   nil,
			},
			expected: map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      "test-1",
				"result":  map[string]interface{}{"test": "result"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON and back to verify structure
			jsonBytes, err := json.Marshal(tt.message)
			if err != nil {
				t.Fatalf("Failed to marshal message: %v", err)
			}

			var unmarshaled map[string]interface{}
			if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
				t.Fatalf("Failed to unmarshal JSON: %v", err)
			}

			// Verify required fields
			for key, expectedValue := range tt.expected {
				actualValue, exists := unmarshaled[key]
				if !exists {
					t.Errorf("Required field '%s' is missing", key)
					continue
				}

				if !reflect.DeepEqual(actualValue, expectedValue) {
					t.Errorf("Field '%s' = %v, expected %v", key, actualValue, expectedValue)
				}
			}

			// Verify jsonrpc version is exactly "2.0"
			if jsonrpc, exists := unmarshaled["jsonrpc"]; !exists || jsonrpc != "2.0" {
				t.Error("jsonrpc field must be exactly '2.0'")
			}
		})
	}
}

func testToolDefinitionSchema(t *testing.T) {
	schemaMap := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query text",
			},
			"top_k": map[string]interface{}{
				"type":        "integer",
				"description": "Number of results to return",
				"minimum":     1,
				"maximum":     100,
				"default":     10,
			},
		},
		"required": []string{"query"},
	}
	var schema jsonschema.Schema
	if b, err := json.Marshal(schemaMap); err == nil {
		_ = json.Unmarshal(b, &schema)
	}

	toolDef := types.MCPToolDefinition{
		Name:        "hybrid_search",
		Description: "Perform hybrid search using BM25 + vector search",
		InputSchema: &schema,
	}

	jsonBytes, err := json.Marshal(toolDef)
	if err != nil {
		t.Fatalf("Failed to marshal tool definition: %v", err)
	}

	var unmarshaled map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal tool definition: %v", err)
	}

	// Verify required fields
	requiredFields := []string{"name", "description", "inputSchema"}
	for _, field := range requiredFields {
		if _, exists := unmarshaled[field]; !exists {
			t.Errorf("Tool definition missing required field: %s", field)
		}
	}

	// Verify name is non-empty string
	if name, ok := unmarshaled["name"].(string); !ok || name == "" {
		t.Error("Tool name must be a non-empty string")
	}

	// Verify description is non-empty string
	if desc, ok := unmarshaled["description"].(string); !ok || desc == "" {
		t.Error("Tool description must be a non-empty string")
	}

	// Verify inputSchema structure
	inputSchema, ok := unmarshaled["inputSchema"].(map[string]interface{})
	if !ok {
		t.Fatal("inputSchema must be an object")
	}

	// Verify inputSchema has type "object"
	if schemaType, ok := inputSchema["type"].(string); !ok || schemaType != "object" {
		t.Error("inputSchema type must be 'object'")
	}

	// Verify inputSchema has properties
	if _, exists := inputSchema["properties"]; !exists {
		t.Error("inputSchema must have properties field")
	}

	// Verify properties is an object
	if properties, ok := inputSchema["properties"].(map[string]interface{}); !ok {
		t.Error("inputSchema properties must be an object")
	} else {
		// Verify query parameter exists and is properly defined
		if queryProp, exists := properties["query"]; !exists {
			t.Error("Tool must have query parameter in properties")
		} else if queryMap, ok := queryProp.(map[string]interface{}); !ok {
			t.Error("Query parameter must be an object")
		} else {
			if queryType, ok := queryMap["type"].(string); !ok || queryType != "string" {
				t.Error("Query parameter type must be 'string'")
			}
		}
	}

	// Verify required array exists and contains query
	if required, ok := inputSchema["required"].([]interface{}); !ok {
		t.Error("inputSchema must have required array")
	} else {
		hasQuery := false
		for _, req := range required {
			if reqStr, ok := req.(string); ok && reqStr == "query" {
				hasQuery = true
				break
			}
		}
		if !hasQuery {
			t.Error("inputSchema required array must contain 'query'")
		}
	}
}

func testRequestMessageFormat(t *testing.T) {
	tests := []struct {
		name    string
		request types.LegacyMCPToolRequest
		valid   bool
	}{
		{
			name: "valid tools/list request",
			request: types.LegacyMCPToolRequest{
				JSONRPC: "2.0",
				ID:      "req-1",
				Method:  "tools/list",
				Params:  nil,
			},
			valid: true,
		},
		{
			name: "valid tools/call request",
			request: types.LegacyMCPToolRequest{
				JSONRPC: "2.0",
				ID:      "req-2",
				Method:  "tools/call",
				Params: types.MCPToolCallParams{
					Name:      "hybrid_search",
					Arguments: map[string]interface{}{"query": "test"},
				},
			},
			valid: true,
		},
		{
			name: "invalid jsonrpc version",
			request: types.LegacyMCPToolRequest{
				JSONRPC: "1.0",
				ID:      "req-3",
				Method:  "tools/list",
			},
			valid: false,
		},
		{
			name: "missing method",
			request: types.LegacyMCPToolRequest{
				JSONRPC: "2.0",
				ID:      "req-4",
				Method:  "",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}

			var unmarshaled map[string]interface{}
			if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
				t.Fatalf("Failed to unmarshal request: %v", err)
			}

			// Validate JSON-RPC 2.0 compliance
			if tt.valid {
				if jsonrpc, ok := unmarshaled["jsonrpc"].(string); !ok || jsonrpc != "2.0" {
					t.Error("Valid request must have jsonrpc: '2.0'")
				}

				if method, ok := unmarshaled["method"].(string); !ok || method == "" {
					t.Error("Valid request must have non-empty method")
				}

				// ID can be string, number, or null, but not missing for requests
				if _, exists := unmarshaled["id"]; !exists {
					t.Error("Request should have id field")
				}
			} else {
				// For invalid requests, check specific validation rules
				if tt.request.JSONRPC != "2.0" {
					jsonrpc, _ := unmarshaled["jsonrpc"].(string)
					if jsonrpc == "2.0" {
						t.Error("Test setup error: invalid request has valid jsonrpc")
					}
				}

				if tt.request.Method == "" {
					method, _ := unmarshaled["method"].(string)
					if method != "" {
						t.Error("Test setup error: invalid request has valid method")
					}
				}
			}
		})
	}
}

func testResponseMessageFormat(t *testing.T) {
	tests := []struct {
		name     string
		response types.MCPToolResponse
		hasError bool
	}{
		{
			name: "successful response",
			response: types.MCPToolResponse{
				JSONRPC: "2.0",
				ID:      "resp-1",
				Result:  map[string]interface{}{"tools": []interface{}{}},
				Error:   nil,
			},
			hasError: false,
		},
		{
			name: "error response",
			response: types.MCPToolResponse{
				JSONRPC: "2.0",
				ID:      "resp-2",
				Result:  nil,
				Error: &types.MCPError{
					Code:    types.MCPErrorMethodNotFound,
					Message: "Method not found",
					Data:    nil,
				},
			},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tt.response)
			if err != nil {
				t.Fatalf("Failed to marshal response: %v", err)
			}

			var unmarshaled map[string]interface{}
			if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			// Verify required fields
			if jsonrpc, ok := unmarshaled["jsonrpc"].(string); !ok || jsonrpc != "2.0" {
				t.Error("Response must have jsonrpc: '2.0'")
			}

			if _, exists := unmarshaled["id"]; !exists {
				t.Error("Response must have id field")
			}

			// Response must have either result or error, but not both
			hasResult := false
			hasErrorField := false

			if _, exists := unmarshaled["result"]; exists {
				hasResult = true
			}

			if errorField, exists := unmarshaled["error"]; exists && errorField != nil {
				hasErrorField = true
			}

			if tt.hasError {
				if !hasErrorField {
					t.Error("Error response must have error field")
				}
				if hasResult {
					t.Error("Error response should not have result field")
				}
			} else {
				if !hasResult {
					t.Error("Success response must have result field")
				}
				if hasErrorField {
					t.Error("Success response should not have error field")
				}
			}
		})
	}
}

func testErrorFormat(t *testing.T) {
	tests := []struct {
		name      string
		mcpError  types.MCPError
		errorCode int
	}{
		{
			name: "parse error",
			mcpError: types.MCPError{
				Code:    types.MCPErrorParseError,
				Message: "Parse error",
				Data:    nil,
			},
			errorCode: types.MCPErrorParseError,
		},
		{
			name: "invalid request error",
			mcpError: types.MCPError{
				Code:    types.MCPErrorInvalidRequest,
				Message: "Invalid request",
				Data:    "Additional error data",
			},
			errorCode: types.MCPErrorInvalidRequest,
		},
		{
			name: "method not found error",
			mcpError: types.MCPError{
				Code:    types.MCPErrorMethodNotFound,
				Message: "Method not found",
				Data:    nil,
			},
			errorCode: types.MCPErrorMethodNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tt.mcpError)
			if err != nil {
				t.Fatalf("Failed to marshal error: %v", err)
			}

			var unmarshaled map[string]interface{}
			if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
				t.Fatalf("Failed to unmarshal error: %v", err)
			}

			// Verify required fields
			if code, ok := unmarshaled["code"].(float64); !ok {
				t.Error("Error must have numeric code field")
			} else if int(code) != tt.errorCode {
				t.Errorf("Error code = %d, expected %d", int(code), tt.errorCode)
			}

			if message, ok := unmarshaled["message"].(string); !ok || message == "" {
				t.Error("Error must have non-empty message field")
			}

			// Data field is optional but must be serializable if present
			if tt.mcpError.Data != nil {
				if _, exists := unmarshaled["data"]; !exists {
					t.Error("Error with data should include data field in JSON")
				}
			}

			// Verify standard JSON-RPC error codes
			errorCodes := []int{
				types.MCPErrorParseError,
				types.MCPErrorInvalidRequest,
				types.MCPErrorMethodNotFound,
				types.MCPErrorInvalidParams,
				types.MCPErrorInternalError,
			}

			validCode := false
			for _, validErrorCode := range errorCodes {
				if tt.errorCode == validErrorCode {
					validCode = true
					break
				}
			}

			if !validCode {
				t.Errorf("Error code %d is not a valid JSON-RPC 2.0 error code", tt.errorCode)
			}
		})
	}
}

func testToolListResponse(t *testing.T) {
	var schema jsonschema.Schema
	if b, err := json.Marshal(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query",
			},
		},
		"required": []string{"query"},
	}); err == nil {
		_ = json.Unmarshal(b, &schema)
	}
	toolDefs := []types.MCPToolDefinition{
		{
			Name:        "hybrid_search",
			Description: "Hybrid search tool",
			InputSchema: &schema,
		},
	}

	listResult := types.MCPToolListResult{
		Tools: toolDefs,
	}

	response := types.MCPToolResponse{
		JSONRPC: "2.0",
		ID:      "list-1",
		Result:  listResult,
		Error:   nil,
	}

	jsonBytes, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal tool list response: %v", err)
	}

	var unmarshaled map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// Verify response structure
	if jsonrpc, ok := unmarshaled["jsonrpc"].(string); !ok || jsonrpc != "2.0" {
		t.Error("Tool list response must have jsonrpc: '2.0'")
	}

	result, ok := unmarshaled["result"].(map[string]interface{})
	if !ok {
		t.Fatal("Tool list response result must be an object")
	}

	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatal("Tool list result must have tools array")
	}

	if len(tools) != 1 {
		t.Errorf("Expected 1 tool, got %d", len(tools))
	}

	// Verify tool structure
	if len(tools) > 0 {
		tool, ok := tools[0].(map[string]interface{})
		if !ok {
			t.Fatal("Tool must be an object")
		}

		requiredToolFields := []string{"name", "description", "inputSchema"}
		for _, field := range requiredToolFields {
			if _, exists := tool[field]; !exists {
				t.Errorf("Tool missing required field: %s", field)
			}
		}

		if name, ok := tool["name"].(string); !ok || name == "" {
			t.Error("Tool name must be non-empty string")
		}
	}
}

func testToolCallResponse(t *testing.T) {
	callResult := types.MCPToolCallResult{
		Content: []types.MCPContent{
			{
				Type: "text",
				Text: "Search completed successfully",
			},
		},
		IsError: false,
	}

	response := types.MCPToolResponse{
		JSONRPC: "2.0",
		ID:      "call-1",
		Result:  callResult,
		Error:   nil,
	}

	jsonBytes, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal tool call response: %v", err)
	}

	var unmarshaled map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// Verify response structure
	if jsonrpc, ok := unmarshaled["jsonrpc"].(string); !ok || jsonrpc != "2.0" {
		t.Error("Tool call response must have jsonrpc: '2.0'")
	}

	result, ok := unmarshaled["result"].(map[string]interface{})
	if !ok {
		t.Fatal("Tool call response result must be an object")
	}

	content, ok := result["content"].([]interface{})
	if !ok {
		t.Fatal("Tool call result must have content array")
	}

	if len(content) == 0 {
		t.Error("Tool call content should not be empty")
	}

	// Verify content structure
	if len(content) > 0 {
		contentItem, ok := content[0].(map[string]interface{})
		if !ok {
			t.Fatal("Content item must be an object")
		}

		if contentType, ok := contentItem["type"].(string); !ok || contentType == "" {
			t.Error("Content item must have non-empty type field")
		}

		// Verify text content has text field
		if contentType, _ := contentItem["type"].(string); contentType == "text" {
			if text, ok := contentItem["text"].(string); !ok || text == "" {
				t.Error("Text content must have non-empty text field")
			}
		}
	}

	// Verify isError field
	if isError, ok := result["isError"].(bool); ok && isError {
		t.Error("Successful tool call should have isError: false or omitted")
	}
}

func TestMCPErrorCodeValues(t *testing.T) {
	// Verify that error codes match JSON-RPC 2.0 specification
	expectedCodes := map[string]int{
		"MCPErrorParseError":     -32700,
		"MCPErrorInvalidRequest": -32600,
		"MCPErrorMethodNotFound": -32601,
		"MCPErrorInvalidParams":  -32602,
		"MCPErrorInternalError":  -32603,
	}

	actualCodes := map[string]int{
		"MCPErrorParseError":     types.MCPErrorParseError,
		"MCPErrorInvalidRequest": types.MCPErrorInvalidRequest,
		"MCPErrorMethodNotFound": types.MCPErrorMethodNotFound,
		"MCPErrorInvalidParams":  types.MCPErrorInvalidParams,
		"MCPErrorInternalError":  types.MCPErrorInternalError,
	}

	for name, expectedCode := range expectedCodes {
		if actualCode, exists := actualCodes[name]; !exists {
			t.Errorf("Missing error code constant: %s", name)
		} else if actualCode != expectedCode {
			t.Errorf("Error code %s = %d, expected %d", name, actualCode, expectedCode)
		}
	}
}

func TestMCPContentTypeValidation(t *testing.T) {
	validContentTypes := []string{"text", "image", "resource"}

	for _, contentType := range validContentTypes {
		t.Run("content_type_"+contentType, func(t *testing.T) {
			content := types.MCPContent{
				Type: contentType,
				Text: "test content",
			}

			jsonBytes, err := json.Marshal(content)
			if err != nil {
				t.Fatalf("Failed to marshal content: %v", err)
			}

			var unmarshaled map[string]interface{}
			if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
				t.Fatalf("Failed to unmarshal content: %v", err)
			}

			if actualType, ok := unmarshaled["type"].(string); !ok || actualType != contentType {
				t.Errorf("Content type = %s, expected %s", actualType, contentType)
			}
		})
	}
}

func TestMCPJSONSerialization(t *testing.T) {
	// Test that all MCP types can be serialized to JSON and back without data loss
	testCases := []struct {
		name string
		data interface{}
	}{
		{
			name: "MCPToolRequest",
			data: types.LegacyMCPToolRequest{
				JSONRPC: "2.0",
				ID:      "test",
				Method:  "tools/list",
				Params:  nil,
			},
		},
		{
			name: "MCPToolResponse",
			data: types.MCPToolResponse{
				JSONRPC: "2.0",
				ID:      "test",
				Result:  map[string]interface{}{"test": "value"},
				Error:   nil,
			},
		},
		{
			name: "MCPError",
			data: types.MCPError{
				Code:    types.MCPErrorInvalidRequest,
				Message: "Test error",
				Data:    "error data",
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			// Serialize to JSON
			jsonBytes, err := json.Marshal(tt.data)
			if err != nil {
				t.Fatalf("Failed to marshal %s: %v", tt.name, err)
			}

			// Verify JSON is valid
			if !json.Valid(jsonBytes) {
				t.Errorf("Invalid JSON generated for %s", tt.name)
			}

			// Verify JSON contains expected structure
			var generic map[string]interface{}
			if err := json.Unmarshal(jsonBytes, &generic); err != nil {
				t.Fatalf("Failed to unmarshal JSON for %s: %v", tt.name, err)
			}

			// The JSON should contain the expected fields without being empty
			if len(generic) == 0 {
				t.Errorf("JSON serialization of %s resulted in empty object", tt.name)
			}
		})
	}
}

// TestMethodNameCompliance verifies that method names follow MCP conventions
func TestMethodNameCompliance(t *testing.T) {
	validMethods := []string{
		"tools/list",
		"tools/call",
		"initialize",
		"notifications/initialized",
	}

	for _, method := range validMethods {
		t.Run("method_"+strings.ReplaceAll(method, "/", "_"), func(t *testing.T) {
			request := types.LegacyMCPToolRequest{
				JSONRPC: "2.0",
				ID:      "test",
				Method:  method,
				Params:  nil,
			}

			jsonBytes, err := json.Marshal(request)
			if err != nil {
				t.Fatalf("Failed to marshal request with method %s: %v", method, err)
			}

			var unmarshaled map[string]interface{}
			if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
				t.Fatalf("Failed to unmarshal request: %v", err)
			}

			if actualMethod, ok := unmarshaled["method"].(string); !ok || actualMethod != method {
				t.Errorf("Method = %s, expected %s", actualMethod, method)
			}
		})
	}
}
