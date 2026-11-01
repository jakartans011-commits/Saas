// Package saasagent provides a Go SDK for TrailSeal API.
//
// # Overview
//
// This SDK provides a typed client for interacting with TrailSeal's
// MCP (Model Context Protocol) Gateway and Audit API.
//
// # Features
//
//   - MCP Gateway: Query and ToolCall modes
//   - Authentication: API Key and JWT
//   - Tenant Management: Create, Delete, Soft Delete, Restore
//   - User Management: Create users
//   - Agent Management: Create, List
//   - Tool Management: Create, Execute
//   - Audit Log: Usage and Audit events
//   - GDPR Compliance: Right to erasure, right to access
//
// # Usage
//
//	client := saasagent.NewClient(
//	    "https://api.trailseal.com",
//	    "sk-xxxxxxxxxxxxx",
//	)
//
//	// Query mode (Natural Language)
//	resp, err := client.MCPQuery("my-tenant", &saasagent.MCPRequest{
//	    Query: "echo hello",
//	})
//
//	// ToolCall mode (Explicit Tool)
//	resp, err := client.MCPToolCall("my-tenant", "echo", map[string]interface{}{
//	    "message": "Hello World!",
//	})
//
// # Error Handling
//
// The SDK returns typed errors (*APIError) that include HTTP status codes
// and server error messages.
//
// # Version
//
// This SDK is versioned at v1.0.0 and follows semantic versioning.
package saasagent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client represents the TrailSeal API client.
type Client struct {
	// BaseURL is the base URL of the TrailSeal API (e.g., "https://api.trailseal.com")
	BaseURL string

	// HTTPClient is the HTTP client used for requests (default: http.Client{Timeout: 30s})
	HTTPClient *http.Client

	// APIKey is the API key for authentication
	APIKey string
}

// NewClient creates a new TrailSeal API client.
//
// Example:
//
//	client := saasagent.NewClient(
//	    "https://api.trailseal.com",
//	    "sk-xxxxxxxxxxxxx",
//	)
func NewClient(baseURL string, apiKey string) *Client {
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		APIKey:     apiKey,
	}
}

// RequestOptions configures an API request.
type RequestOptions struct {
	Method  string
	Path    string
	Body    interface{}
	Headers map[string]string
}

// do executes an HTTP request with the given options.
func (c *Client) do(opts RequestOptions) (*http.Response, error) {
	var bodyReader io.Reader
	if opts.Body != nil {
		data, err := json.Marshal(opts.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(opts.Method, c.BaseURL+opts.Path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	return c.HTTPClient.Do(req)
}

// LoginRequest represents login credentials.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse represents login response.
type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// Login authenticates a user and returns access and refresh tokens.
//
// Example:
//
//	resp, err := client.Login("user@example.com", "password123")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println("Access token:", resp.AccessToken)
func (c *Client) Login(email, password string) (*LoginResponse, error) {
	resp, err := c.do(RequestOptions{
		Method: "POST",
		Path:   "/api/auth/login",
		Body:   LoginRequest{Email: email, Password: password},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RefreshToken refreshes the access token using a refresh token.
//
// Example:
//
//	resp, err := client.RefreshToken(refreshToken)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println("New access token:", resp.AccessToken)
func (c *Client) RefreshToken(refreshToken string) (*LoginResponse, error) {
	resp, err := c.do(RequestOptions{
		Method: "POST",
		Path:   "/api/auth/refresh",
		Body:   map[string]string{"refresh_token": refreshToken},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Tenant represents a tenant.
type Tenant struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// CreateTenant creates a new tenant (root only).
//
// Example:
//
//	tenant, err := client.CreateTenant("my-tenant", "My Company")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println("Tenant created:", tenant.ID)
func (c *Client) CreateTenant(tenantID, name string) (*Tenant, error) {
	resp, err := c.do(RequestOptions{
		Method: "POST",
		Path:   "/api/admin/tenants",
		Body:   map[string]string{"tenant_id": tenantID, "name": name},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result struct {
		Tenant Tenant `json:"tenant"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result.Tenant, nil
}

// User represents a user.
type User struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Email    string `json:"email"`
	RoleID   string `json:"role_id"`
	Status   string `json:"status"`
}

// CreateUser creates a new user in a tenant.
//
// Example:
//
//	user, err := client.CreateUser("my-tenant", "newuser@example.com", "password123", "admin")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println("User created:", user.ID)
func (c *Client) CreateUser(tenantID, email, password, roleID string) (*User, error) {
	resp, err := c.do(RequestOptions{
		Method: "POST",
		Path:   "/api/tenants/" + tenantID + "/users",
		Body:   map[string]string{"email": email, "password": password, "role_id": roleID},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result struct {
		User User `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result.User, nil
}

// UsageEvent represents a usage event.
type UsageEvent struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id"`
	EventType string            `json:"event_type"`
	Metadata  map[string]string `json:"metadata"`
	Timestamp time.Time         `json:"timestamp"`
}

// GetUsage gets usage events for a tenant.
//
// Example:
//
//	events, err := client.GetUsage("my-tenant", 50)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Found %d events\n", len(events))
func (c *Client) GetUsage(tenantID string, limit int) ([]UsageEvent, error) {
	path := "/api/tenants/" + tenantID + "/usage"
	if limit > 0 {
		path = path + "?limit=" + fmt.Sprintf("%d", limit)
	}

	resp, err := c.do(RequestOptions{
		Method: "GET",
		Path:   path,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result struct {
		Events []UsageEvent `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Events, nil
}

// Agent represents an agent.
type Agent struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Status       string            `json:"status"`
	Capabilities []string          `json:"capabilities"`
	Metadata     map[string]string `json:"metadata"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// CreateAgent creates a new agent.
//
// Example:
//
//	agent, err := client.CreateAgent("my-tenant", "My Agent", "Does stuff", []string{"read"}, nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println("Agent created:", agent.ID)
func (c *Client) CreateAgent(tenantID, name, description string, capabilities []string, metadata map[string]string) (*Agent, error) {
	resp, err := c.do(RequestOptions{
		Method: "POST",
		Path:   "/api/tenants/" + tenantID + "/agents",
		Body: map[string]interface{}{
			"name":         name,
			"description":  description,
			"capabilities": capabilities,
			"metadata":     metadata,
		},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, c.parseError(resp)
	}

	var result struct {
		Agent Agent `json:"agent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result.Agent, nil
}

// ListAgents lists agents for a tenant.
//
// Example:
//
//	agents, err := client.ListAgents("my-tenant")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, a := range agents {
//	    fmt.Println(a.Name)
//	}
func (c *Client) ListAgents(tenantID string) ([]Agent, error) {
	resp, err := c.do(RequestOptions{
		Method: "GET",
		Path:   "/api/tenants/" + tenantID + "/agents",
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result struct {
		Agents []Agent `json:"agents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Agents, nil
}

// Tool represents a tool.
type Tool struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenant_id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Type        string            `json:"type"`
	Config      map[string]string `json:"config"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// CreateTool creates a new tool.
//
// Supported tool types: echo, health_check, metadata_lookup, audit_lookup
//
// Example:
//
//	tool, err := client.CreateTool("my-tenant", "echo", "Echo messages", "echo", nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println("Tool created:", tool.ID)
func (c *Client) CreateTool(tenantID, name, description, toolType string, config map[string]string) (*Tool, error) {
	resp, err := c.do(RequestOptions{
		Method: "POST",
		Path:   "/api/tenants/" + tenantID + "/tools",
		Body: map[string]interface{}{
			"name":        name,
			"description": description,
			"type":        toolType,
			"config":      config,
		},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, c.parseError(resp)
	}

	var result struct {
		Tool Tool `json:"tool"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result.Tool, nil
}

// ExecuteTool executes a tool with given parameters.
//
// Example:
//
//	result, err := client.ExecuteTool("my-tenant", "tool-id", map[string]interface{}{
//	    "message": "Hello!",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(result)
func (c *Client) ExecuteTool(tenantID, toolID string, params map[string]interface{}) (map[string]interface{}, error) {
	resp, err := c.do(RequestOptions{
		Method: "POST",
		Path:   "/api/tenants/" + tenantID + "/tools/" + toolID + "/execute",
		Body:   map[string]interface{}{"params": params},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// ============================================================
// MCP GATEWAY
// ============================================================

// MCPToolCall represents an explicit tool-call request.
//
// Use this when the tool to invoke is already known.
type MCPToolCall struct {
	// ToolName is the name or ID of the tool to call
	ToolName string `json:"tool_name"`
	// Params are the parameters for the tool
	Params map[string]interface{} `json:"params,omitempty"`
}

// MCPRequest represents an MCP query request.
//
// Two modes are supported:
//  1. Query mode: set Query (and optionally Params/Context)
//  2. Explicit tool-call mode: set ToolCall (takes priority over Query)
type MCPRequest struct {
	// Query is the natural language query (Query mode)
	Query string `json:"query,omitempty"`
	// Context is optional context for the query
	Context map[string]string `json:"context,omitempty"`
	// Params are optional parameters for the query
	Params map[string]interface{} `json:"params,omitempty"`
	// ToolCall is the explicit tool call (ToolCall mode)
	ToolCall *MCPToolCall `json:"tool_call,omitempty"`
}

// MCPResponse represents an MCP query response.
type MCPResponse struct {
	// Response is the result of the MCP query
	Response string `json:"response"`
	// ToolsUsed are the tools that were used to generate the response
	ToolsUsed []string `json:"tools_used"`
	// ExecutionTimeMs is the execution time in milliseconds
	ExecutionTimeMs int64 `json:"execution_time_ms"`
	// Metadata contains additional information (tenant_id, request_id, etc.)
	Metadata map[string]string `json:"metadata"`
}

// MCPQuery sends an MCP query to the MCP Gateway (Query Mode).
//
// This is the natural language mode where the query will be interpreted
// by the agent and executed against available tools.
//
// Example:
//
//	resp, err := client.MCPQuery("my-tenant", &saasagent.MCPRequest{
//	    Query: "echo hello",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(resp.Response)
func (c *Client) MCPQuery(tenantID string, req *MCPRequest) (*MCPResponse, error) {
	resp, err := c.do(RequestOptions{
		Method: "POST",
		Path:   "/api/tenants/" + tenantID + "/mcp/query",
		Body:   req,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result MCPResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// MCPToolCall sends an explicit tool-call MCP request (ToolCall Mode).
//
// Use this when the tool to invoke is already known, instead of
// relying on Query-based resolution.
//
// Example:
//
//	resp, err := client.MCPToolCall("my-tenant", "echo", map[string]interface{}{
//	    "message": "Hello World!",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(resp.Response)
func (c *Client) MCPToolCall(tenantID string, toolName string, params map[string]interface{}) (*MCPResponse, error) {
	return c.MCPQuery(tenantID, &MCPRequest{
		ToolCall: &MCPToolCall{
			ToolName: toolName,
			Params:   params,
		},
	})
}

// ============================================================
// GDPR COMPLIANCE
// ============================================================

// DeletionStatus represents GDPR deletion status.
type DeletionStatus struct {
	Status    string     `json:"status"`
	TenantID  string     `json:"tenant_id"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	Message   string     `json:"message"`
}

// SoftDeleteTenant soft-deletes a tenant (GDPR right to erasure).
//
// Example:
//
//	status, err := client.SoftDeleteTenant("my-tenant")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println("Status:", status.Status)
func (c *Client) SoftDeleteTenant(tenantID string) (*DeletionStatus, error) {
	resp, err := c.do(RequestOptions{
		Method: "POST",
		Path:   "/api/tenants/" + tenantID + "/data/delete",
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result DeletionStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RestoreTenant restores a soft-deleted tenant.
//
// Example:
//
//	status, err := client.RestoreTenant("my-tenant")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println("Status:", status.Status)
func (c *Client) RestoreTenant(tenantID string) (*DeletionStatus, error) {
	resp, err := c.do(RequestOptions{
		Method: "POST",
		Path:   "/api/tenants/" + tenantID + "/data/restore",
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result DeletionStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ============================================================
// ERROR HANDLING
// ============================================================

// APIError is a typed error returned by the TrailSeal API.
//
// It includes:
//   - Code: A stable error code for programmatic handling
//   - Message: A human-readable error message
//   - StatusCode: The HTTP status code
//
// Example:
//
//	if err != nil {
//	    apiErr, ok := err.(*saasagent.APIError)
//	    if ok {
//	        switch apiErr.Code {
//	        case "tool_not_found":
//	            // Handle tool not found
//	        case "rate_limited":
//	            // Handle rate limiting
//	        }
//	    }
//	}
type APIError struct {
	Code       string
	Message    string
	StatusCode int
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("API error [%s]: %s (status %d)", e.Code, e.Message, e.StatusCode)
}

// apiErrorCodeForStatus maps HTTP status codes to stable error codes.
func apiErrorCodeForStatus(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "unauthenticated"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "tool_not_found"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusInternalServerError:
		return "internal_error"
	default:
		return "unknown_error"
	}
}

// parseError parses the error response from the API.
func (c *Client) parseError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &APIError{
			Code:       apiErrorCodeForStatus(resp.StatusCode),
			Message:    fmt.Sprintf("failed to read error body: %v", err),
			StatusCode: resp.StatusCode,
		}
	}

	var errResp struct {
		Error string `json:"error"`
	}

	message := string(body)
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		message = errResp.Error
	}

	return &APIError{
		Code:       apiErrorCodeForStatus(resp.StatusCode),
		Message:    message,
		StatusCode: resp.StatusCode,
	}
}