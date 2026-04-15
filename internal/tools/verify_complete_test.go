package tools

import (
	"context"
	"strings"
	"testing"
)

func TestVerifyCompleteTool_Execute_Validation(t *testing.T) {
	tool := &VerifyCompleteTool{}
	ctx := context.Background()

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
		contains string
	}{
		{
			name: "missing both args",
			args: map[string]interface{}{},
			contains: "Error: 'summary' 및 'verification_evidence' 인자가 비어 있습니다",
		},
		{
			name: "empty summary",
			args: map[string]interface{}{
				"summary":               "",
				"verification_evidence": "some evidence",
			},
			contains: "Error: 'summary' 및 'verification_evidence' 인자가 비어 있습니다",
		},
		{
			name: "valid args",
			args: map[string]interface{}{
				"summary":               "done something",
				"verification_evidence": "looked at logs",
			},
			contains: "작업 완료가 시스템에 공식 접수되었습니다",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tool.Execute(ctx, tt.args, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(got, tt.contains) {
				t.Errorf("Execute() got = %v, want to contain %v", got, tt.contains)
			}
		})
	}
}
