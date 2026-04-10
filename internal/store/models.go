// Package store
// File: models.go
// Description: 서버 저장소에서 사용하는 도메인 모델 타입 정의
// Responsibility: Server 구조체와 AuthType 열거형 제공

package store

import "time"

// AuthType은 SSH 인증 방식을 나타낸다.
type AuthType string

const (
	// AuthTypeKey는 SSH 개인키 파일 인증을 나타낸다.
	AuthTypeKey AuthType = "key"
	// AuthTypePassword는 비밀번호 인증을 나타낸다.
	AuthTypePassword AuthType = "password"
)

// Server는 등록된 원격 서버의 접속 정보를 나타낸다.
// Credential 필드: AuthTypeKey면 키 파일 경로(평문), AuthTypePassword면 암호화된 비밀번호.
type Server struct {
	ID         int
	Name       string    // 사용자 지정 별칭 (예: "db-server")
	Host       string    // IP 또는 FQDN
	Port       int       // SSH 포트 (기본 22)
	User       string    // SSH 사용자명
	AuthType   AuthType  // "key" 또는 "password"
	Credential string    // AuthTypeKey: 키 경로, AuthTypePassword: AES 암호화된 값
	OS         string    // 스캔된 운영체제 정보 (예: "Ubuntu 22.04")
	EnvProfile string    // 자동 스캔된 주요 환경 정보 (예: "Docker, MySQL")
	CreatedAt  time.Time
}
