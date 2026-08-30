package impl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sapcc/ucfgwrap"

	"github.com/sapcc/maintenance-controller/plugin"
)

// Request is a trigger plugin that sends an HTTP request to a configured endpoint.
type Request struct {
	URL    string
	Method string
	Body   string
}

// New creates a new Request instance with the given config.
func (r *Request) New(config *ucfgwrap.Config) (plugin.Trigger, error) {
	conf := struct {
		URL    string `config:"url" validate:"required"`
		Method string `config:"method"`
		Body   string `config:"body"`
	}{
		Method: "POST", // Default to POST for backward compatibility
	}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &Request{
		URL:    conf.URL,
		Method: conf.Method,
		Body:   conf.Body,
	}, nil
}

func (r *Request) ID() string {
	return "request"
}

// Trigger sends an HTTP request to the configured URL.
// Supports placeholders: {{nodeName}}, {{state}}, {{startsAt}}, {{endsAt}}, {{silenceId}}.
func (r *Request) Trigger(params plugin.Parameters) error {
	// Replace placeholders in URL
	url := r.replacePlaceholders(r.URL, params)
	
	// Replace placeholders in body
	body := r.replacePlaceholders(r.Body, params)

	client := &http.Client{Timeout: 30 * time.Second}

	// Create request
	var req *http.Request
	var err error
	if r.Body != "" {
		req, err = http.NewRequest(r.Method, url, strings.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequest(r.Method, url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("request to %s returned status %d: %s", url, resp.StatusCode, string(respBody))
	}

	params.Log.Info("Request trigger executed", "url", url, "method", r.Method, "status", resp.StatusCode)
	
	// For POST requests to AlertManager, extract and store silence ID
	if r.Method == "POST" && strings.Contains(url, "/silences") {
		if err := r.extractAndStoreSilenceID(respBody, params); err != nil {
			params.Log.Error(err, "Failed to extract silence ID")
			// Don't fail the trigger, just log the error
		}
	}
	
	return nil
}

// replacePlaceholders replaces all placeholders in the given string.
func (r *Request) replacePlaceholders(s string, params plugin.Parameters) string {
	s = strings.ReplaceAll(s, "{{nodeName}}", params.Node.Name)
	s = strings.ReplaceAll(s, "{{state}}", params.State)
	
	// Generate timestamps
	now := time.Now().UTC()
	startsAt := now.Format(time.RFC3339)
	endsAt := now.Add(48 * time.Hour).Format(time.RFC3339) // 48 hour maintenance window
	s = strings.ReplaceAll(s, "{{startsAt}}", startsAt)
	s = strings.ReplaceAll(s, "{{endsAt}}", endsAt)
	
	// Get silence ID from node label if exists
	if silenceID, ok := params.Node.Labels["cloud.sap/alertmanager-silence-id"]; ok {
		s = strings.ReplaceAll(s, "{{silenceId}}", silenceID)
	}
	
	return s
}

// extractAndStoreSilenceID extracts the silence ID from AlertManager response and stores it as a node label.
func (r *Request) extractAndStoreSilenceID(respBody []byte, params plugin.Parameters) error {
	var response struct {
		SilenceID string `json:"silenceID"`
	}
	
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	
	if response.SilenceID == "" {
		return fmt.Errorf("no silenceID in response")
	}
	
	params.Log.Info("Storing silence ID in node label", "silenceID", response.SilenceID)
	
	// Store silence ID as a node label
	node := params.Node.DeepCopy()
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	node.Labels["cloud.sap/alertmanager-silence-id"] = response.SilenceID
	
	ctx := context.Background()
	err := params.Client.Update(ctx, node)
	if err != nil {
		return fmt.Errorf("failed to update node label with silence ID: %w", err)
	}
	
	return nil
}