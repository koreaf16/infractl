# MCP 통합

## 개요

MCP(Model Context Protocol)는 Claude Code가 외부 도구 서버와 통신하는 표준 프로토콜이다.  
Claude Code는 MCP 클라이언트이자 (MCP 서버 모드에서) 서버로도 동작한다.

---

## MCP 서버 유형

`services/mcp/types.ts`에 정의된 서버 연결 방식:

| 타입 | 연결 방식 | 예시 |
|------|-----------|------|
| `stdio` | 서브프로세스 stdin/stdout | `npx @modelcontextprotocol/server-filesystem` |
| `http` | HTTP (SSE 또는 스트리밍) | `https://my-mcp-server.com/mcp` |
| `sse` | Server-Sent Events | 레거시 방식 |
| `websocket` | WebSocket | 실시간 양방향 |

```typescript
type McpServerConfig =
  | McpStdioServerConfig    // { command, args, env }
  | McpHTTPServerConfig     // { url, headers }
  | McpSSEServerConfig      // { url } — 레거시
  | McpWebSocketServerConfig // { url }
```

---

## 설정 방법

### settings.json

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path"],
      "env": { "API_KEY": "..." }
    },
    "my-http-server": {
      "url": "https://my-server.com/mcp",
      "headers": { "Authorization": "Bearer ${MY_TOKEN}" }
    }
  }
}
```

**환경변수 확장:** `${VAR_NAME}` 구문으로 환경변수 값을 주입할 수 있다.

### 설정 범위 (ConfigScope)

| 범위 | 위치 | 영향 |
|------|------|------|
| `local` | `.claude/settings.local.json` | 현재 프로젝트 (개인) |
| `project` | `settings.json` | 현재 프로젝트 (공유) |
| `user` | `~/.claude/settings.json` | 모든 프로젝트 |
| `managed` | `/etc/claude-code/managed-mcp.json` | 기업 전체 (읽기 전용) |

---

## MCP 클라이언트 초기화

`services/mcp/client.ts`의 `getMcpToolsCommandsAndResources()`가 모든 MCP 서버에 연결한다.

```
getMcpToolsCommandsAndResources(mcpConfigs)
    │
    ├─ 각 서버에 connectToServer() 병렬 실행
    │       ├─ stdio: spawn() → StdioClientTransport
    │       ├─ http: SSEClientTransport 또는 StreamableHTTPClientTransport
    │       └─ ws: WebSocketClientTransport
    │
    ├─ fetchToolsForClient() → 서버의 도구 목록 조회
    ├─ 도구를 Tool 인터페이스 래퍼로 변환
    └─ { tools, commands, resources } 반환
```

### MCPServerConnection 상태

```typescript
type MCPServerConnection = {
  name: string
  client: Client
  config: ScopedMcpServerConfig
  tools: Tool[]
  resources: ServerResource[]
  commands: Command[]
  status: 'connected' | 'connecting' | 'error' | 'not-connected'
  error?: string
}
```

---

## MCP 도구 실행

MCP 도구는 일반 도구와 동일한 `Tool` 인터페이스를 구현한다.

```typescript
// MCP 도구 호출 시
mcpTool.call(input, context)
    │
    └─ client.callTool({ name, arguments: input })
            │
            └─ MCP 서버 응답 → ToolResult 변환
```

**MCP 도구 이름 규칙:** `mcp__<serverName>__<toolName>`  
예: `mcp__filesystem__read_file`

---

## MCP 인증 (auth.ts)

`services/mcp/auth.ts` (91KB)는 OAuth 2.0 PKCE 흐름을 구현한다.

```
MCP 서버 인증 요청 (-32042 오류)
    │
    ├─ OAuth 인증 URL 생성
    ├─ 브라우저 열기 (또는 URL 출력)
    ├─ 콜백 서버 대기 (localhost:임시포트)
    ├─ 인증 코드 수신
    └─ 토큰 교환 → 저장 (keychain 또는 파일)
```

**저장 위치:** `~/.claude/mcp-auth/<serverName>/tokens.json`

---

## Elicitation 처리 (elicitationHandler.ts)

MCP 서버가 추가 정보를 요청할 때의 처리.

```typescript
// MCP 서버 → -32042 오류 + ElicitRequestURLParams
// → handleElicitation() 호출
// REPL 모드: UI 다이얼로그 표시
// SDK 모드: structuredIO.handleElicitation() 위임
```

---

## MCP 리소스 도구

Claude Code는 MCP 리소스 접근을 위한 내장 도구를 제공한다.

```typescript
// ListMcpResourcesTool
// → 연결된 MCP 서버들의 리소스 목록 조회
// → 예: "filesystem 서버의 /home/user 디렉토리"

// ReadMcpResourceTool
// → 특정 MCP 리소스 내용 읽기
// → 서버명 + 리소스 URI 지정
```

---

## 채널 권한 (channelPermissions.ts)

MCP 서버별 권한을 별도로 관리한다.

```typescript
type ChannelPermissionCallbacks = {
  checkToolPermission: (serverName, toolName, input) => PermissionResult
  // MCP 서버 이름을 포함한 권한 규칙 매칭
  // "mcp__filesystem__write_file" 형태의 규칙 지원
}
```

---

## MCP 서버 모드 (entrypoints/mcp.ts)

`--mcp-server` 플래그로 Claude Code 자체를 MCP 서버로 노출한다.

```
외부 IDE (VS Code, JetBrains 등)
    │ MCP 프로토콜
    ▼
Claude Code (MCP 서버 모드)
    ├─ 내장 도구들을 MCP 도구로 노출
    ├─ BashTool, FileEditTool 등
    └─ IDE에서 Claude를 MCP 도구 서버로 사용
```

---

## 공식 MCP 레지스트리

```typescript
// services/mcp/officialRegistry.ts
prefetchOfficialMcpUrls()
// → 공식 MCP 서버 목록 사전 로드
// → /mcp 명령에서 검색 및 설치에 활용
```

---

## 관련 문서

- [03-도구-시스템](./03-도구-시스템.md) — MCP 도구 동적 등록
- [16-설정-구성](./16-설정-구성.md) — MCP 서버 설정
- [17-인증-API-클라이언트](./17-인증-API-클라이언트.md) — OAuth 인증 흐름
