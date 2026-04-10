# 인증 및 API 클라이언트

## 개요

Claude Code는 세 가지 인증 방식을 지원하며, 우선순위에 따라 자동으로 선택한다.

| 우선순위 | 방식 | 조건 |
|---------|------|------|
| 1 | API 키 (`ANTHROPIC_API_KEY`) | 환경변수 설정 시 |
| 2 | OAuth 토큰 (claude.ai) | 브라우저 로그인 완료 시 |
| 3 | AWS Bedrock | `CLAUDE_CODE_USE_BEDROCK=1` |
| 4 | GCP Vertex AI | `CLAUDE_CODE_USE_VERTEX=1` |

---

## 인증 흐름 (auth.ts)

`utils/auth.ts` (67KB)가 전체 인증 로직을 담당한다.

### API 키 인증

```typescript
// 우선순위 순으로 탐색
1. ANTHROPIC_API_KEY 환경변수
2. 파일 디스크립터 (--api-key-fd 플래그)
3. ~/.claude/settings.json의 apiKey
4. macOS Keychain (레거시)
```

### OAuth 인증 (claude.ai)

```typescript
// services/oauth/client.ts
shouldUseClaudeAIAuth()  // OAuth 사용 여부 판단

isOAuthTokenExpired(tokens)  // 토큰 만료 확인
refreshOAuthToken(tokens)    // 리프레시 토큰으로 갱신

// 토큰 저장: macOS Keychain 또는 ~/.claude/auth.json
```

**OAuth 흐름:**
1. `claude login` 실행 → 브라우저 열기
2. claude.ai에서 승인 → 콜백 URL로 코드 수신
3. 코드 교환 → 액세스/리프레시 토큰 저장

### Bedrock 인증

```typescript
// utils/aws.ts
prefetchAwsCredentialsAndBedRockInfoIfSafe()
// → AWS SDK 자격증명 (IAM Role, Access Key 등)
// → 모델 ARN 자동 해석
checkStsCallerIdentity()  // STS로 자격증명 검증
```

### Vertex AI 인증

```typescript
prefetchGcpCredentialsIfSafe()
// → Google Application Default Credentials
// → 서비스 계정 키 또는 gcloud auth
```

---

## API 클라이언트 (services/api/claude.ts)

`services/api/claude.ts` (129KB)는 `@anthropic-ai/sdk`를 래핑한다.

### 클라이언트 생성

```typescript
// services/api/client.ts
createAnthropicClient({
  apiKey,
  baseURL,      // ANTHROPIC_BASE_URL 오버라이드
  defaultHeaders: {
    'anthropic-beta': betas.join(','),  // 베타 기능 헤더
    'X-App-Name': 'claude-code',
  },
})
```

### API 호출 (claude.ts)

```typescript
// 스트리밍 메시지 생성
streamMessage({
  model,
  system,
  messages,
  tools,
  max_tokens,
  thinking: thinkingConfig,  // Extended Thinking
  budget_tokens,             // 사고 토큰 예산
})
```

### 모델별 최대 토큰

```typescript
getMaxOutputTokensForModel(model)
// → claude-opus-4-6: 32,000
// → claude-sonnet-4-6: 64,000
// → 기타 기본: 8,192
```

---

## 재시도 로직 (withRetry.ts)

`services/api/withRetry.ts`가 일시적 오류를 처리한다.

```typescript
// 재시도 조건
categorizeRetryableAPIError(error)
// → 'retryable': 서버 오류 (5xx), 과부하 (529), 네트워크 오류
// → 'non_retryable': 클라이언트 오류 (4xx, 인증 실패 등)
// → 'abort': 사용자 취소

// 재시도 전략
// - 지수 백오프 (exponential backoff)
// - 최대 재시도 횟수: 3회 (기본)
// - FallbackTriggeredError: 폴백 모델로 전환
```

### FallbackTriggeredError

기본 모델이 실패하면 폴백 모델로 전환한다.

```typescript
// --fallback-model CLI 플래그로 설정
// FallbackTriggeredError 발생 시 QueryEngineConfig.fallbackModel로 재시도
```

---

## 프롬프트 캐싱

Anthropic API의 프롬프트 캐싱 기능을 활용한다.

```typescript
// services/api/promptCacheBreakDetection.ts
// 프롬프트 캐시 히트율 추적
// 캐시 미스 시 자동 알림
notifyCompaction()  // 컴팩트 후 캐시 무효화 처리
```

**캐시 제어 헤더:**

```typescript
// 시스템 프롬프트에 cache_control: { type: 'ephemeral' } 추가
// → 동일 시스템 프롬프트 재사용 시 입력 토큰 절감
```

---

## API 로깅 (logging.ts)

`services/api/logging.ts`가 API 사용량을 추적한다.

```typescript
type NonNullableUsage = {
  input_tokens: number
  output_tokens: number
  cache_creation_input_tokens: number
  cache_read_input_tokens: number
}

// bootstrap/state.ts에 누적
getTotalInputTokens()
getTotalOutputTokens()
getTotalCacheCreationInputTokens()
getTotalCacheReadInputTokens()
```

---

## API 오류 처리 (errors.ts)

```typescript
// services/api/errors.ts
PROMPT_TOO_LONG_ERROR_MESSAGE      // 컨텍스트 초과
isPromptTooLongMessage(error)      // 프롬프트 길이 초과 판별

// 주요 오류 코드
// 401: 인증 실패 → 재로그인 안내
// 402: 크레딧 부족 → 결제 페이지 안내
// 403: 권한 없음 → 정책 확인 안내
// 529: 서버 과부하 → 재시도
```

---

## 구독 타입 및 제한

```typescript
// services/claudeAiLimits.ts
checkQuotaStatus()         // 사용량 할당 확인
getSubscriptionType()      // 'free' | 'pro' | 'team' | 'enterprise'
isClaudeAISubscriber()     // claude.ai 구독자 여부
```

---

## API 엔드포인트

| 제공자 | 기본 URL |
|--------|---------|
| Anthropic | `https://api.anthropic.com` |
| Bedrock | `https://bedrock-runtime.<region>.amazonaws.com` |
| Vertex | `https://<region>-aiplatform.googleapis.com` |
| 커스텀 | `ANTHROPIC_BASE_URL` 환경변수 |

---

## 관련 문서

- [16-설정-구성](./16-설정-구성.md) — 인증 관련 설정
- [01-시작점과-부트스트랩](./01-시작점과-부트스트랩.md) — Keychain 프리패치
- [02-쿼리-엔진](./02-쿼리-엔진.md) — API 호출 흐름
