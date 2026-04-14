// Package tools
// File: rag_register.go
// Description: rag_register 도구 — 외부 DB 벡터 테이블을 RAG 소스로 등록
// Responsibility: LLM이 대화로 수집한 접속 정보를 rag_sources에 저장

package tools

import (
	"context"
	"fmt"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/store"
)

// RAGRegisterTool은 외부 DB 벡터 테이블을 RAG 소스로 등록하는 도구이다.
type RAGRegisterTool struct {
	Store store.RAGSourceStore
}

var _ Tool = (*RAGRegisterTool)(nil)

func (t *RAGRegisterTool) Name() string         { return "rag_register" }
func (t *RAGRegisterTool) RiskLevel() RiskLevel { return RiskLow }
func (t *RAGRegisterTool) IsReadOnly() bool     { return false }
func (t *RAGRegisterTool) IsEnabled() bool      { return true }

func (t *RAGRegisterTool) Description() string {
	return "외부 DB의 벡터 테이블을 RAG 소스로 등록합니다. " +
		"사용자가 '사내 매뉴얼 RAG로 등록', 'RAG 소스 추가' 등을 요청할 때 사용합니다. " +
		"등록 후 rag_search 도구가 해당 DB에서 벡터 검색을 수행합니다."
}

func (t *RAGRegisterTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "RAG 소스 이름 (테이블명 또는 용도명)",
			},
			"server_name": map[string]interface{}{
				"type":        "string",
				"description": "대상 서버 이름 (server_list로 확인)",
			},
			"db_type": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"oracle", "mysql", "postgresql"},
				"description": "DB 종류",
			},
			"db_name": map[string]interface{}{
				"type":        "string",
				"description": "Oracle SID 또는 MySQL/PostgreSQL DB명",
			},
			"table_name": map[string]interface{}{
				"type":        "string",
				"description": "벡터 테이블명",
			},
			"text_column": map[string]interface{}{
				"type":        "string",
				"description": "검색 텍스트 컬럼명",
			},
			"vector_column": map[string]interface{}{
				"type":        "string",
				"description": "벡터 임베딩 컬럼명",
			},
			"result_columns": map[string]interface{}{
				"type":        "string",
				"description": "결과에 포함할 컬럼 목록 (콤마 구분, 예: doc_text,category)",
			},
			"embedding_model": map[string]interface{}{
				"type":        "string",
				"description": "벡터 생성에 사용된 임베딩 모델명 (예: all-minilm). 현재 설정과 불일치 시 경고.",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "용도 설명 (예: 사내 운영 매뉴얼)",
			},
			"username": map[string]interface{}{
				"type":        "string",
				"description": "DB 접속 계정",
			},
			"password": map[string]interface{}{
				"type":        "string",
				"description": "DB 접속 비밀번호",
			},
			"priority": map[string]interface{}{
				"type":        "integer",
				"description": "검색 우선순위 (1=가장 먼저, 숫자가 클수록 나중). 기본값: 1",
			},
		},
		"required": []string{"name", "server_name", "db_type", "db_name", "table_name",
			"text_column", "vector_column", "result_columns", "embedding_model", "username", "password"},
	}
}

func (t *RAGRegisterTool) Execute(ctx context.Context, args map[string]interface{}, _ executor.Executor) (string, error) {
	name, _ := args["name"].(string)
	serverName, _ := args["server_name"].(string)
	dbType, _ := args["db_type"].(string)
	dbName, _ := args["db_name"].(string)
	tableName, _ := args["table_name"].(string)
	textCol, _ := args["text_column"].(string)
	vecCol, _ := args["vector_column"].(string)
	resultCols, _ := args["result_columns"].(string)
	embModel, _ := args["embedding_model"].(string)
	desc, _ := args["description"].(string)
	username, _ := args["username"].(string)
	password, _ := args["password"].(string)

	if name == "" || serverName == "" || dbType == "" || tableName == "" ||
		username == "" || password == "" {
		return "필수 파라미터(name, server_name, db_type, table_name, username, password)가 누락되었습니다", nil
	}

	priority := 1
	if v, ok := args["priority"].(float64); ok && int(v) > 0 {
		priority = int(v)
	}

	source := store.RAGSource{
		Name:           name,
		ServerName:     serverName,
		DBType:         dbType,
		DBName:         dbName,
		TableName:      tableName,
		TextColumn:     textCol,
		VectorColumn:   vecCol,
		ResultColumns:  resultCols,
		EmbeddingModel: embModel,
		Description:    desc,
		Priority:       priority,
		Credentials: store.RAGSourceCredentials{
			Username: username,
			Password: password,
		},
	}

	id, err := t.Store.SaveRAGSource(ctx, source)
	if err != nil {
		return fmt.Sprintf("RAG 소스 등록 실패: %s", err), nil
	}

	return fmt.Sprintf(
		"✓ RAG 소스 등록 완료 (ID: %d)\n"+
			"이름: %s\n"+
			"서버: %s / %s / %s\n"+
			"우선순위: %d순위\n"+
			"앞으로 rag_search 도구가 이 소스를 %d순위로 검색합니다.",
		id, name, serverName, dbType, tableName, priority, priority,
	), nil
}
