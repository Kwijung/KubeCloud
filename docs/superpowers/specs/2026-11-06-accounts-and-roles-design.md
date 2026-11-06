# 계정과 역할 설계

작성일: 2026-11-06

도입 시점: 기본 기능을 모두 구현한 뒤. 이 문서는 그때 구현할 설계이고, 그 전에는 API와 콘솔에 인증이 없다.

## 1. 목표

Kloudia에 로그인과 권한을 넣는다.

- 계정은 사람마다 하나. 역할로 권한을 나눈다.
- 일반 사용자는 멤버로 들어간 프로젝트만 보고 쓴다.
- 관리자는 사용자 기능을 모두 쓰고, 관리자 모드에서 클러스터·사양·공용 이미지·프로젝트·사용자를 관리한다.

OpenStack Horizon과 같은 구성이다: 역할이 admin인 사용자에게만 관리 패널이 보이고, 프로젝트 화면과 관리 화면이 나뉜다.

## 2. 범위

### 포함

- 자체 계정 로그인 (아이디 + 비밀번호), 로그아웃, 세션
- 플랫폼 역할 `admin` / `user`
- 프로젝트 멤버십 (관리자가 추가·제거)
- 사용자 관리: 생성(임시 비밀번호), 비활성화·활성화, 비밀번호 초기화, 역할 변경
- 프로젝트 관리: 생성, 이름·설명 수정
- 첫 로그인 시 비밀번호 변경 강제, 본인 비밀번호 변경
- API 권한 검사와 콘솔의 관리자 모드

### 제외

- 회원가입, 이메일 비밀번호 찾기 (계정은 관리자만 만든다)
- 외부 인증(OIDC / Keycloak). 인증 부분을 분리해 두고 나중에 붙인다.
- 프로젝트 안의 세부 역할(소유자 / 멤버 / 읽기 전용). 지금은 멤버면 그 프로젝트의 모든 리소스를 다룬다.
- 프로젝트 쿼터, API 토큰, 2단계 인증

## 3. 데이터 모델

```
users
  id              text PRIMARY KEY          -- 로그인 아이디 (영문 소문자, 숫자, 하이픈)
  display_name    text NOT NULL
  password_hash   text NOT NULL             -- bcrypt
  role            text NOT NULL             -- 'admin' | 'user'
  disabled        boolean NOT NULL DEFAULT false
  must_change_password boolean NOT NULL DEFAULT true
  created_at, last_login_at

sessions
  id_hash         text PRIMARY KEY          -- 세션 토큰의 SHA-256. 토큰 원문은 저장하지 않음
  user_id         text REFERENCES users(id) ON DELETE CASCADE
  expires_at      timestamptz NOT NULL
  created_at

project_members
  project_id      text REFERENCES projects(id) ON DELETE CASCADE
  user_id         text REFERENCES users(id) ON DELETE CASCADE
  added_at
  PRIMARY KEY (project_id, user_id)
```

- 관리자는 멤버십 없이 모든 프로젝트에 접근한다.
- 사용자를 비활성화하면 그 사용자의 세션을 모두 지운다.
- 마지막 활성 관리자는 비활성화하거나 역할을 내릴 수 없다 (409).

## 4. 인증

- 비밀번호: `golang.org/x/crypto/bcrypt`, cost 12. 최소 길이 10자.
- 로그인 `POST /api/v1/auth/login` 성공 시 무작위 32바이트 토큰을 발급해 쿠키 `kloudia_session`에 담는다. 쿠키 속성: `HttpOnly`, `Secure`(HTTPS일 때), `SameSite=Lax`, `Path=/`.
- 세션 수명: 기본 8시간, "로그인 상태 유지"를 고르면 12시간. 고정 만료이며 사용할 때마다 늘리지 않는다.
- 로그인 실패는 아이디가 없는 경우와 비밀번호가 틀린 경우를 구분하지 않고 같은 메시지를 준다. 같은 아이디로 5분에 10번 넘게 실패하면 그 아이디의 로그인을 5분간 막는다.
- 상태를 바꾸는 요청(POST/PUT/DELETE)은 `Origin`이 콘솔 주소와 같아야 한다 (CSRF 방어). 허용 주소는 설정값 `KLOUDIA_CONSOLE_ORIGIN`.
- **콘솔 WebSocket**: 지금 `ConsoleStreamer`는 `CheckOrigin`이 항상 true다. 인증을 넣을 때 세션 쿠키 확인과 함께 같은 Origin 검사를 적용한다. 쿠키 인증만 넣고 Origin 검사를 빼면 다른 사이트가 사용자 브라우저로 콘솔에 붙을 수 있다.
- 첫 관리자: 사용자가 한 명도 없을 때 백엔드가 시작하면 `KLOUDIA_BOOTSTRAP_ADMIN_PASSWORD`로 `admin` 계정을 만든다. 값이 없으면 시작을 거부한다. 이 계정도 첫 로그인 때 비밀번호를 바꾸게 한다.
- 인증은 `Authenticator` 인터페이스(요청 → 사용자) 뒤에 두어, 나중에 OIDC 구현을 붙일 수 있게 한다.

## 5. 권한

| 경로 | 허용 |
|---|---|
| `/api/v1/auth/*` | 누구나 (로그인), 로그인한 사용자 (로그아웃, 내 정보, 비밀번호 변경) |
| `/api/v1/projects` (GET) | 로그인한 사용자. user는 멤버인 프로젝트만, admin은 전체 |
| `/api/v1/projects/{p}/...` | 그 프로젝트 멤버 또는 admin. 아니면 404 (프로젝트 존재 여부를 숨김) |
| `/api/v1/flavors` (GET) | 로그인한 사용자 |
| `/api/v1/admin/...` | admin. 아니면 403 |
| `/api/v1/preflight` | admin (클러스터 내부 정보) |
| `/healthz`, `/api/v1/health` | 누구나 |

`must_change_password`가 true인 세션은 비밀번호 변경과 로그아웃 외의 요청에 403 `PASSWORD_CHANGE_REQUIRED`를 받는다.

## 6. 관리자 API

| 메서드·경로 | 동작 |
|---|---|
| `GET/POST /api/v1/admin/users` | 목록, 생성 (아이디·이름·임시 비밀번호·역할·선택 프로젝트) |
| `PATCH /api/v1/admin/users/{id}` | 이름·역할·비활성화 변경 |
| `POST /api/v1/admin/users/{id}/reset-password` | 임시 비밀번호 설정, 그 사용자의 세션 삭제 |
| `GET/POST /api/v1/admin/projects` | 목록(멤버 수·VM 수 포함), 생성 |
| `PATCH /api/v1/admin/projects/{id}` | 이름·설명 수정 |
| `GET/POST /api/v1/admin/projects/{id}/members` | 멤버 목록, 추가 |
| `DELETE /api/v1/admin/projects/{id}/members/{userId}` | 멤버 제거 |

공용 이미지와 사양 관리 API도 `/api/v1/admin/` 아래에 둔다 ([공용 이미지 설계](2026-11-06-public-and-project-images-design.md)).

## 7. 콘솔

- 로그인 화면. 회원가입과 비밀번호 찾기 링크는 없고 "계정은 관리자가 만듭니다" 안내만 둔다.
- 상단 프로젝트 선택기는 내가 멤버인 프로젝트만 보인다 (admin은 전체).
- **관리자 모드 스위치**: admin에게만 상단에 보인다. 켜면 사이드바가 관리자 메뉴(클러스터, 사양, 공용 이미지, 프로젝트, 사용자)로 바뀌고 상단에 경고색 띠와 "관리자 모드" 배지가 붙는다. 상태는 브라우저에만 저장하며 권한과는 무관하다 (권한은 서버가 역할로 판단).
- 지금 프로젝트 화면에 있는 클러스터 점검(preflight) 패널은 관리자 화면으로 옮긴다.
- 화면 시안: Claude Design 캔버스 "Kloudia Console"의 로그인·관리자 화면.

## 8. 도입 순서

1. 스키마(users, sessions, project_members)와 첫 관리자 생성
2. 로그인·로그아웃·세션 미들웨어, Origin 검사(콘솔 WebSocket 포함)
3. 프로젝트 경로 권한 검사, 프로젝트 목록 필터
4. 관리자 API (사용자, 프로젝트, 멤버)
5. 콘솔: 로그인 화면, 관리자 모드, 관리자 화면

기존 데이터: 지금 있는 프로젝트(platform, data)는 그대로 두고, 첫 관리자가 사용자를 만들어 멤버로 넣는다.

## 9. 테스트

- 인증: 로그인 성공·실패(같은 메시지), 잠금, 만료 세션 거부, 비활성 사용자 거부, 비밀번호 변경 강제.
- 권한: 멤버 아닌 프로젝트 404, user의 관리자 경로 403, admin의 모든 프로젝트 접근, 마지막 관리자 보호.
- CSRF: 다른 Origin의 POST와 콘솔 WebSocket 거부.
