package security

import (
	"fmt"
	"net/url"
	"strings"
)

// allowedCodeLanguages are the only languages run_code accepts.
var allowedCodeLanguages = map[string]bool{
	"python3": true,
	"go":      true,
	"node":    true,
	"bash":    true,
}

// ValidateToolInput checks tool inputs against allowlists before execution.
// Prevents the LLM from hallucinating dangerous tool calls.
func ValidateToolInput(toolName string, input map[string]any) error {
	switch toolName {
	case "write_file":
		return validateWriteFile(input)
	case "fetch_url":
		return validateFetchURL(input)
	case "run_code":
		return validateRunCode(input)
	case "ghl_send_message":
		return validateGHLSendMessage(input)
	case "send_email":
		return validateSendEmail(input)
	}
	return nil // unknown tools pass through
}

func validateWriteFile(input map[string]any) error {
	path, _ := input["path"].(string)
	if path == "" {
		return fmt.Errorf("write_file: path is required")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("write_file: path must not contain '..'")
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("write_file: path must not be absolute (starts with '/')")
	}
	return nil
}

func validateFetchURL(input map[string]any) error {
	rawURL, _ := input["url"].(string)
	if rawURL == "" {
		return fmt.Errorf("fetch_url: url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("fetch_url: invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("fetch_url: URL must use http:// or https:// scheme, got %q", parsed.Scheme)
	}
	return nil
}

func validateRunCode(input map[string]any) error {
	lang, _ := input["language"].(string)
	if lang == "" {
		return fmt.Errorf("run_code: language is required")
	}
	if !allowedCodeLanguages[lang] {
		return fmt.Errorf("run_code: language %q not allowed (allowed: python3, go, node, bash)", lang)
	}
	return nil
}

func validateGHLSendMessage(input map[string]any) error {
	contactID, _ := input["contactId"].(string)
	message, _ := input["message"].(string)
	if contactID == "" {
		return fmt.Errorf("ghl_send_message: contactId is required")
	}
	if message == "" {
		return fmt.Errorf("ghl_send_message: message is required")
	}
	return nil
}

func validateSendEmail(input map[string]any) error {
	to, _ := input["to"].(string)
	if to == "" {
		return fmt.Errorf("send_email: 'to' field is required")
	}
	// Basic email format check: must contain @ with text on both sides.
	if !strings.Contains(to, "@") || strings.HasPrefix(to, "@") || strings.HasSuffix(to, "@") {
		return fmt.Errorf("send_email: 'to' field %q is not a valid email address", to)
	}
	parts := strings.SplitN(to, "@", 2)
	if !strings.Contains(parts[1], ".") {
		return fmt.Errorf("send_email: 'to' field %q is not a valid email address", to)
	}
	return nil
}
