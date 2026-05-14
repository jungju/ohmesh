# ohmesh 소개 페이지 기획

## 목표

`ohmesh`를 처음 본 개발자가 "정적 프론트엔드 앱에 로그인과 사용자별 JSON 저장소를 붙이는 작은 백엔드"라고 바로 이해하게 만든다. 소개 페이지는 마케팅용 과장보다 실제 사용 흐름, 신뢰, 간단함을 앞세운다.

핵심 메시지:

- 정적 앱은 화면과 API 호출에 집중한다.
- OAuth 로그인, HttpOnly 쿠키 세션, redirect/domain 검증은 ohmesh가 맡는다.
- 사용자별 JSON CRUD 저장소를 SQLite 하나로 단순하게 제공한다.
- v1은 하나의 Go 서비스와 하나의 SQLite 데이터베이스로 충분히 작게 간다.

## 대상 사용자

- GitHub Pages, Cloudflare Pages, Netlify, Vercel static export 등에 작은 앱을 올리는 개인 개발자
- 노트, 투두, 대시보드, 내부 도구처럼 사용자별 저장소가 필요한 프론트엔드 앱 제작자
- 별도 백엔드, DB, 세션 처리를 직접 만들고 싶지 않은 프로토타입/사이드 프로젝트 개발자

## 톤과 방향

- 톤: 실용적이고 조용하게 믿음직한 개발자 도구
- 비주얼: 밝은 실제 제품 사진 느낌, 절제된 UI, 흰색/회색 바탕에 청록색 포인트
- 피할 것: 과장된 SaaS 랜딩, 추상 3D 오브젝트만 있는 히어로, OAuth 토큰이나 세션 토큰이 노출된 듯한 표현, GitHub/Google 로고를 사진 안에 직접 생성하는 것

## 페이지 구조

### 1. Hero

역할: 첫 화면에서 제품 정체성과 효용을 즉시 전달한다.

추천 카피:

- H1: `ohmesh`
- 서브헤드: `정적 앱에 로그인과 사용자별 JSON 저장소를 붙이는 가장 작은 백엔드`
- 본문: `GitHub Pages나 커스텀 도메인에 올린 앱에서 OAuth 로그인, 쿠키 세션, 사용자별 JSON CRUD를 바로 사용하세요. 앱은 화면에 집중하고 인증과 데이터 스코핑은 ohmesh가 맡습니다.`
- CTA: `로그인`, `앱 관리`, `API 문서`

비주얼: 전체 폭 히어로 사진 위에 텍스트를 올리거나, 넓은 배경 사진과 오른쪽 제품 UI 미리보기를 결합한다. 브랜드명은 사진 안이 아니라 실제 HTML 텍스트로 보여준다.

### 2. 문제 제기

역할: 정적 앱이 막히는 순간을 짧게 짚는다.

섹션 제목: `정적 앱은 쉬운데, 로그인부터 복잡해집니다`

포인트:

- OAuth provider 설정과 callback 처리
- 쿠키 세션과 CORS/credential 요청
- 사용자별 데이터 격리
- 앱별 허용 도메인과 redirect URL 검증

### 3. 작동 방식

역할: 사용자가 머릿속으로 흐름을 그릴 수 있게 한다.

섹션 제목: `앱 slug 하나로 이어지는 로그인 흐름`

3단계:

1. 정적 앱이 `/login?app={slug}&redirect_url={url}`로 사용자를 보낸다.
2. ohmesh가 GitHub/Google OAuth와 앱 전용 세션 쿠키를 처리한다.
3. 앱은 `credentials: "include"`로 `/auth/me`와 `/api/apps/{slug}/records`를 호출한다.

이 섹션은 사진보다 단순한 HTML/CSS 다이어그램이 더 좋다. 실제 토큰 문자열은 절대 노출하지 않는다.

### 4. 핵심 기능

역할: "어디까지 대신 해주는가"를 빠르게 확인하게 한다.

카드 4개:

- `OAuth 로그인`: GitHub/Google OAuth 시작과 callback 처리
- `앱별 세션`: 앱 전용 HttpOnly 쿠키로 사용자 로그인 유지
- `도메인 검증`: 등록된 도메인과 redirect URL만 허용
- `JSON 저장소`: 사용자와 앱 범위로 격리된 JSON CRUD

### 5. 사용 사례

역할: ohmesh가 붙을 수 있는 앱의 종류를 상상하게 한다.

예시:

- 개인 노트/저널 앱
- 투두/습관 추적기
- 작은 CRM 또는 운영 대시보드
- 이벤트 RSVP, 북마크, 폼 결과 저장소
- GitHub Pages에 올린 포트폴리오의 로그인 영역

### 6. 개발자 빠른 시작

역할: 소개 페이지가 실제 개발로 바로 이어지게 한다.

포함할 코드:

```js
await fetch("https://ohmesh.example.com/auth/me?app=notes", {
  credentials: "include"
})
```

```js
await fetch("https://ohmesh.example.com/api/apps/notes/records", {
  method: "POST",
  credentials: "include",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({
    type: "note",
    data: { title: "Hello", done: false }
  })
})
```

### 7. 신뢰와 운영

역할: 작은 서비스지만 인증/데이터 경계를 신경 쓴다는 점을 명확히 한다.

포인트:

- OAuth access token, refresh token, raw session token은 API 응답에 반환하지 않는다.
- 레코드는 앱과 현재 사용자 세션 기준으로 스코프된다.
- 유연한 앱 데이터는 검증된 JSON text로 저장한다.
- v1은 Go 서비스 하나와 SQLite 하나로 운영한다.
- Kubernetes는 선택 배포 패키징일 뿐, 제품 구조를 쪼개지 않는다.

### 8. 마지막 CTA

역할: 사용자가 다음 행동을 선택하게 한다.

추천 카피:

- 제목: `정적 앱에 작은 백엔드를 붙여보세요`
- 본문: `앱을 등록하고 redirect URL을 연결하면, 로그인과 사용자별 저장소를 바로 사용할 수 있습니다.`
- CTA: `앱 등록하기`, `API 가이드 보기`

## 사진 구성

총 6장을 권장한다. 페이지 전체가 사진으로 과밀해지지 않도록 히어로 1장, 섹션 보조 4장, 마무리 1장으로 배치한다.

| 번호 | 파일명 제안 | 배치 | 역할 |
| --- | --- | --- | --- |
| 1 | `intro-hero.webp` | Hero | 정적 앱과 작은 API 백엔드가 연결되는 제품 첫인상 |
| 2 | `static-app-workbench.webp` | 문제 제기 | 정적 앱 개발자의 작업 맥락 |
| 3 | `oauth-handoff.webp` | 작동 방식 보조 | 로그인 흐름과 안전한 인증 handoff |
| 4 | `json-storage.webp` | 핵심 기능 | 사용자별 JSON 데이터 저장소 이미지 |
| 5 | `app-gallery.webp` | 사용 사례 | 여러 정적 앱에 공통 백엔드를 붙이는 느낌 |
| 6 | `small-deploy.webp` | 마지막 CTA/운영 | 작고 단순한 배포/운영 감각 |

생성 가이드:

- 실제 페이지 텍스트는 HTML로 올리고, 사진 안에는 읽을 수 있는 문자를 넣지 않는다.
- OAuth provider 로고, 브랜드 로고, 실제 회사 로고는 생성하지 않는다.
- 사람 얼굴이 필요 없다면 넣지 않는다. 넣더라도 식별 가능한 유명인처럼 보이면 안 된다.
- 모든 이미지의 색감은 밝고 선명하게, 청록색 포인트만 약하게 반복한다.
- 기본 비율은 `16:9`, 카드형 보조 이미지는 `4:3`, 모바일 크롭을 고려해 주요 피사체를 중앙 70% 안에 둔다.

## 이미지 프롬프트

아래 프롬프트는 그대로 이미지 생성 도구에 넣기 좋게 영어로 작성했다.

### 1. Hero: `intro-hero.webp`

```text
A polished realistic product photograph for a developer tool landing page, a modern laptop on a clean desk showing an abstract static web app interface connected to a small central API service, subtle teal accents, bright natural daylight, white and soft gray environment, premium but practical, no readable text, no brand logos, no OAuth provider logos, no people, shallow depth of field, 16:9 composition, main subject centered for responsive cropping
```

### 2. 문제 제기: `static-app-workbench.webp`

```text
Realistic photograph of a compact developer workspace with a laptop, browser windows represented as clean abstract panels, a small notebook with simple wireframe sketches, a calm practical atmosphere, daylight, neutral white and gray tones with a small teal accent object, no readable text, no logos, no faces, not a stock office scene, 4:3 composition
```

### 3. 작동 방식: `oauth-handoff.webp`

```text
Realistic close-up product photo of a laptop and phone showing an abstract secure login handoff flow, simple lock icon shapes and connected interface panels, calm trustworthy mood, clean white background, soft teal highlights, no readable text, no real company logos, no GitHub or Google logos, no token strings, no passwords, 4:3 composition
```

### 4. 핵심 기능: `json-storage.webp`

```text
Realistic macro-style photograph of a developer monitor with abstract JSON-like blocks and small separated user data containers, visual metaphor for scoped per-user storage, clean technical aesthetic, white desk, teal and graphite accents, no readable code, no secret tokens, no brand logos, crisp focus, 4:3 composition
```

### 5. 사용 사례: `app-gallery.webp`

```text
Realistic product photograph showing several devices on a desk, each displaying a different abstract static app interface such as notes, tasks, dashboard, and bookmarks, all visually connected to one small backend hub, bright clean developer-tool style, neutral background with teal accents, no readable text, no logos, no people, 16:9 composition
```

### 6. 운영/CTA: `small-deploy.webp`

```text
Realistic photograph of a small simple deployment setup for a lightweight web service, a laptop beside a compact mini server or single-board computer and a clean network cable arrangement, calm reliable mood, bright daylight, white and gray palette with subtle teal accent light, no readable terminal text, no brand logos, no people, 16:9 composition
```

## 구현 메모

- 현재 서버는 `templates/*.tmpl`만 embed하고 있으므로 실제 이미지를 넣을 때는 정적 파일 제공 경로가 필요하다.
- 추천 경로는 `internal/server/static/intro/`이고, 구현 시 `embed.FS` 또는 Gin static handler 중 하나로 일관되게 제공한다.
- 이미지 파일은 원본을 그대로 쓰지 말고 `webp`로 압축한다. 히어로는 약 `1600px` 폭, 보조 이미지는 약 `1000px` 폭이면 충분하다.
- 첫 화면에서는 브랜드/제품명이 즉시 보여야 하므로 H1은 `ohmesh` 또는 `정적 앱을 위한 로그인과 JSON 저장소`처럼 직접적인 문구가 좋다.
- 사진은 보조 설명용이고 핵심 기능 설명은 HTML 텍스트와 간단한 다이어그램으로 유지한다.
