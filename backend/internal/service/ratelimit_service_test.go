package service

import (
	"testing"
)

func TestCheckFatal400Error(t *testing.T) {
	s := &RateLimitService{}

	tests := []struct {
		name         string
		responseBody []byte
		wantFatal    bool
		wantContains string
	}{
		{
			name:         "organization has been disabled",
			responseBody: []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"This organization has been disabled."}}`),
			wantFatal:    true,
			wantContains: "Organization disabled (400)",
		},
		{
			name:         "organization is disabled",
			responseBody: []byte(`{"error":{"message":"The organization is disabled"}}`),
			wantFatal:    true,
			wantContains: "Organization disabled (400)",
		},
		{
			name:         "account has been disabled",
			responseBody: []byte(`{"error":{"message":"Your account has been disabled"}}`),
			wantFatal:    true,
			wantContains: "Account disabled (400)",
		},
		{
			name:         "api key has been disabled",
			responseBody: []byte(`{"error":{"message":"This API key has been disabled"}}`),
			wantFatal:    true,
			wantContains: "API key disabled (400)",
		},
		{
			name:         "workspace has been disabled",
			responseBody: []byte(`{"error":{"message":"This workspace has been disabled"}}`),
			wantFatal:    true,
			wantContains: "Workspace disabled (400)",
		},
		{
			name:         "normal 400 error - invalid request",
			responseBody: []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: must be greater than 0"}}`),
			wantFatal:    false,
			wantContains: "",
		},
		{
			name:         "normal 400 error - missing parameter",
			responseBody: []byte(`{"error":{"message":"Missing required parameter: model"}}`),
			wantFatal:    false,
			wantContains: "",
		},
		{
			name:         "empty response body",
			responseBody: []byte{},
			wantFatal:    false,
			wantContains: "",
		},
		{
			name:         "case insensitive - ORGANIZATION HAS BEEN DISABLED",
			responseBody: []byte(`{"error":{"message":"This ORGANIZATION HAS BEEN DISABLED"}}`),
			wantFatal:    true,
			wantContains: "Organization disabled (400)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.checkFatal400Error(tt.responseBody)

			if tt.wantFatal {
				if result == "" {
					t.Errorf("checkFatal400Error() = empty, want fatal error containing %q", tt.wantContains)
				} else if tt.wantContains != "" && !contains(result, tt.wantContains) {
					t.Errorf("checkFatal400Error() = %q, want containing %q", result, tt.wantContains)
				}
			} else {
				if result != "" {
					t.Errorf("checkFatal400Error() = %q, want empty (not fatal)", result)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
