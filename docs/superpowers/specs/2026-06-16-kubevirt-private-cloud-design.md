# KubeVirt 기반 사내 Private Cloud 플랫폼 기획

작성일: 2026-06-16

## 1. 목표

기존 VM 기반 Kubernetes 클러스터 위에 KubeVirt/CDI를 설치하고, 사내 사용자가 웹 포털에서 VM을 셀프서비스로 생성하고 관리할 수 있는 Private Cloud 플랫폼을 구현한다.

초기 목표는 전체 클라우드 기능을 한 번에 구현하는 것이 아니라, KubeVirt 기반 VM 생성/접속/운영 흐름을 제품 형태로 닫는 것이다. 따라서 먼저 인프라 spike로 KubeVirt 실행 조건을 검증한 뒤, 검증된 리소스 모델을 기준으로 Portal MVP를 구현한다.

## 2. 현재 환경 가정

- Kubernetes 버전: 1.36.1
- 클러스터 구성:
  - control-plane 1대 또는 3대
  - worker 여러 대
  - control-plane은 VM workload 실행 대상에서 제외
- 테스트 환경:
  - Kubernetes 클러스터는 이미 있음
  - KubeVirt/CDI는 아직 설치되지 않음
  - 테스트 클러스터는 VM 위에서 실행됨
  - nested virtualization 플래그는 확인됨: `egrep -c '(vmx|svm)' /proc/cpuinfo` 결과 `4`
- CNI: Cilium
- Multus: 아직 설치 안 됨
- Storage:
  - NFS CSI 사용 가능
  - StorageClass/PVC 동적 생성 가능

## 3. MVP 범위

### 포함

- KubeVirt/CDI 설치 및 검증 스크립트
- NFS CSI StorageClass 기반 VM 디스크 생성
- URL import 기반 프로젝트 이미지 등록
- OIDC 로그인
- Keycloak을 플랫폼 구성요소로 제공
- 프로젝트 단위 테넌시
- 프로젝트 Owner/Member 권한
- 프로젝트별 vCPU/Memory/Disk 쿼터
- Flavor 기반 VM 생성 wizard
- cloud-init 기반 SSH public key 주입
- VM 목록/상세/생성/시작/정지/삭제
- KubeVirt subresource API 기반 웹 콘솔 프록시
- 기본 상태 표시:
  - VM phase
  - IP
  - 실행 노드
  - vCPU/Memory/Disk 요청량
  - 이미지 import 상태
  - PVC/DataVolume 상태
- 주요 실패 사유 표시:
  - 스케줄링 실패
  - 이미지 import 실패
  - PVC 생성 실패
  - VM 부팅 실패

### 제외

- Multus 기반 프로젝트별 사설 VM 네트워크
- VLAN 연동
- 외부 IP, 포트포워딩, 방화벽 정책
- 스냅샷/백업
- Prometheus 기반 사용량 차트
- 브라우저 대용량 이미지 업로드
- 자체 CRD/operator
- 과금
- 신청/승인 워크플로

## 4. 접근 방식

선택한 접근 방식은 Infra Spike 이후 Portal MVP 구현이다.

### 1단계: Infra Spike

목적은 앱 개발 전에 KubeVirt 실행 리스크를 제거하는 것이다.

검증 항목:

- KubeVirt stable 버전과 Kubernetes 1.36.1 호환성
- worker 노드의 `/dev/kvm`, `/dev/net/tun`, `vhost-net`
- privileged workload 실행 가능 여부
- NFS CSI StorageClass로 PVC 생성 가능 여부
- CDI DataVolume URL import 가능 여부
- cloud-init으로 SSH key 주입 가능 여부
- VM 생성/시작 후 Running 상태 도달 여부
- VM stop/delete lifecycle 검증은 Portal MVP/API 테스트 또는 후속 lifecycle 테스트에서 수행
- KubeVirt console/VNC subresource 접근 가능 여부

제공 스크립트:

- `scripts/check-cluster-prereqs.sh`
- `scripts/install-kubevirt.sh`
- `scripts/install-cdi.sh`
- `scripts/smoke-test-vm.sh`

### 2단계: Portal MVP

Infra Spike에서 검증된 Kubernetes/KubeVirt 리소스 모델을 바탕으로 Go API와 React 포털을 구현한다.

## 5. 아키텍처

```text
[React Portal]
   |
   | HTTPS / API
   v
[Go Backend API]
   |-- Auth/OIDC JWT 검증
   |-- Project / Role / Quota 정책
   |-- Image / VM / SSH Key API
   |-- Audit Log
   |
   | Kubernetes client-go / kubevirt client-go
   v
[Kubernetes Cluster]
   |-- KubeVirt: VirtualMachine / VMI / subresources
   |-- CDI: DataVolume import
   |-- PVC: NFS CSI StorageClass
   |-- Secret: cloud-init / SSH key material
   |
   v
[Worker Nodes with /dev/kvm]
```

### Frontend

- React + TypeScript
- 프로젝트 선택
- VM 목록/상세
- 이미지 등록
- VM 생성 wizard
- SSH key 관리
- 콘솔 화면
- API 상태 관리는 TanStack Query 사용

### Backend API

- Go
- HTTP framework: chi
- OIDC JWT 검증
- 프로젝트 Owner/Member 권한 체크
- 쿼터 검증
- VM/Image/SSH key API 제공
- Kubernetes/KubeVirt/CDI 리소스 생성과 조회
- 감사 로그 기록

### PostgreSQL

플랫폼 정책과 메타데이터의 source of truth로 사용한다.

저장 대상:

- 사용자 프로필 캐시
- 프로젝트
- 프로젝트 멤버십
- Flavor
- 이미지 메타데이터
- SSH public key
- 쿼터 설정
- 감사 로그

### Kubernetes/KubeVirt

실제 실행 상태의 source of truth로 사용한다.

조회 대상:

- VM running 상태
- VMI phase
- IP
- 실행 노드
- PVC/DataVolume 상태
- Kubernetes Event

## 6. 기술 스택

- Backend: Go
- HTTP: chi
- Kubernetes 연동: `client-go`
- KubeVirt 연동: `kubevirt/client-go`
- Frontend: React + TypeScript
- DB: PostgreSQL
- Auth: Keycloak 기반 OIDC
- Packaging:
  - 여러 Helm chart로 분리
  - platform, postgres, keycloak chart 분리
- Infra 설치:
  - KubeVirt/CDI는 플랫폼 chart와 분리
  - 설치/검증 스크립트로 제공

## 7. 배포 구성

Helm chart는 다음처럼 분리한다.

- `charts/kloudia-platform`
  - API
  - Web
  - ServiceAccount/RBAC
  - Config/Secret 참조
  - Ingress
- `charts/kloudia-postgres`
  - 개발/테스트용 PostgreSQL
  - 외부 PostgreSQL 연결 옵션 제공
- `charts/kloudia-keycloak`
  - 테스트/소규모용 Keycloak
  - 외부 Keycloak 연결 옵션 제공

KubeVirt/CDI 설치는 chart에 묶지 않고 `scripts/`와 문서로 관리한다.

## 8. 권한 모델

권한은 MVP에서 프로젝트 Owner/Member만 둔다.

### Owner

- 프로젝트 멤버 관리
- 프로젝트 이미지 등록/삭제
- VM 생성/시작/정지/삭제
- VM 콘솔 접근
- VM 상세 조회
- 프로젝트 SSH key 관리

### Member

- VM 생성
- VM 시작/정지
- VM 콘솔 접근
- VM 상세 조회
- 본인 SSH key 관리

플랫폼 전체 Admin 권한은 MVP에서 별도 사용자 역할로 확장하지 않는다. 초기 운영 작업은 설치/관리 스크립트와 제한된 내부 운영 계정으로 처리한다.

## 9. 데이터 모델 초안

### users

- id
- oidc_subject
- email
- display_name
- created_at
- last_login_at

### projects

- id
- name
- description
- created_at

### project_members

- id
- project_id
- user_id
- role: `owner` 또는 `member`

### project_quotas

- project_id
- cpu_limit
- memory_limit_bytes
- disk_limit_bytes

### flavors

- id
- name
- cpu
- memory_bytes
- root_disk_bytes
- enabled

### images

- id
- project_id
- name
- os_type
- source_url
- datavolume_name
- pvc_name
- status
- error_message
- created_by
- created_at

### ssh_keys

- id
- user_id
- name
- public_key
- fingerprint
- created_at

### virtual_machines

- id
- project_id
- name
- namespace
- kubevirt_vm_name
- image_id
- flavor_id
- cpu
- memory_bytes
- disk_bytes
- ssh_key_id
- status_cache
- created_by
- created_at

### audit_logs

- id
- actor_user_id
- project_id
- action
- target_type
- target_id
- result
- message
- created_at

## 10. Kubernetes 리소스 매핑

프로젝트는 Kubernetes namespace와 1:1 매핑한다.

예:

- 프로젝트: `dev-platform`
- Namespace: `kloudia-prj-dev-platform`

리소스:

- VM: KubeVirt `VirtualMachine`
- 실행 인스턴스: KubeVirt `VirtualMachineInstance`
- 이미지 import: CDI `DataVolume`
- 디스크: PVC
- SSH/cloud-init: Secret

공통 label:

- `kloudia.io/project-id`
- `kloudia.io/vm-id`
- `kloudia.io/image-id`
- `kloudia.io/managed-by=kloudia`

## 11. 주요 API 초안

### Auth/User

- `GET /api/me`

### Projects

- `GET /api/projects`
- `GET /api/projects/{projectId}`
- `GET /api/projects/{projectId}/members`
- `POST /api/projects/{projectId}/members`
- `DELETE /api/projects/{projectId}/members/{userId}`

### Flavors

- `GET /api/flavors`

### Images

- `GET /api/projects/{projectId}/images`
- `POST /api/projects/{projectId}/images`
- `GET /api/projects/{projectId}/images/{imageId}`
- `DELETE /api/projects/{projectId}/images/{imageId}`

### SSH Keys

- `GET /api/ssh-keys`
- `POST /api/ssh-keys`
- `DELETE /api/ssh-keys/{keyId}`

### Virtual Machines

- `GET /api/projects/{projectId}/vms`
- `POST /api/projects/{projectId}/vms`
- `GET /api/projects/{projectId}/vms/{vmId}`
- `POST /api/projects/{projectId}/vms/{vmId}/start`
- `POST /api/projects/{projectId}/vms/{vmId}/stop`
- `DELETE /api/projects/{projectId}/vms/{vmId}`
- `GET /api/projects/{projectId}/vms/{vmId}/events`

### Console

- `GET /api/projects/{projectId}/vms/{vmId}/console`

콘솔 엔드포인트는 HTTP upgrade 또는 WebSocket 기반 프록시로 구현한다. 백엔드는 요청 시 프로젝트 권한을 확인한 뒤 KubeVirt subresource API로 연결한다.

## 12. VM 생성 흐름

1. 사용자가 프로젝트 선택
2. VM 생성 wizard 진입
3. 이미지 선택
4. Flavor 선택
5. SSH key 선택
6. 이름과 디스크 설정 확인
7. Backend가 Owner/Member 권한 확인
8. Backend가 프로젝트 쿼터 확인
9. Backend가 cloud-init Secret 생성
10. Backend가 VM용 PVC/DataVolume 또는 이미지 PVC clone 경로 생성
11. Backend가 KubeVirt VirtualMachine 생성
12. Frontend가 VM 상태를 polling 또는 watch 기반으로 표시

## 13. 에러 처리

Backend는 Kubernetes/KubeVirt/CDI 오류를 사용자에게 그대로 노출하지 않고 플랫폼 에러 코드로 매핑한다.

예:

- `QUOTA_EXCEEDED`
- `IMAGE_IMPORT_FAILED`
- `PVC_PROVISIONING_FAILED`
- `VM_SCHEDULING_FAILED`
- `VM_START_FAILED`
- `CONSOLE_CONNECTION_FAILED`
- `FORBIDDEN`

VM 상세 화면에는 사람이 이해할 수 있는 실패 사유와 관련 이벤트 일부를 표시한다.

## 14. 테스트 전략

### Infra Spike 검증

- Kubernetes 버전 확인
- worker node KVM 확인
- KubeVirt 설치
- CDI 설치
- NFS CSI StorageClass PVC 생성
- URL import DataVolume 생성
- 테스트 VM 생성/start/Running 확인
- SSH key 주입 확인
- console subresource 접근 확인

### Backend 테스트

- 권한 체크 단위 테스트
- 쿼터 계산 단위 테스트
- Kubernetes adapter fake client 테스트
- VM 생성 요청 validation 테스트
- VM stop/delete lifecycle API 테스트
- 이미지 import 상태 매핑 테스트

### Frontend 테스트

- VM 생성 wizard 상태 전이 테스트
- API error 표시 테스트
- 권한별 버튼/액션 표시 테스트

### 통합 테스트

- kind/minikube는 KubeVirt 조건이 제한적이므로 실제 VM 기반 테스트 클러스터를 기준 환경으로 둔다.
- CI에서는 backend/frontend 단위 테스트를 우선 실행하고, KubeVirt 통합 테스트는 별도 환경에서 수동 또는 nightly로 실행한다.

## 15. 후속 확장

- Multus 기반 프로젝트별 사설 네트워크
- VLAN 연동
- 외부 IP/포트포워딩
- NetworkPolicy/방화벽 정책
- Prometheus 기반 VM 사용량 차트
- Snapshot/backup/restore
- 브라우저 파일 업로드 기반 이미지 등록
- 프로젝트 비용/사용량 리포트
- 신청/승인 워크플로
- 자체 CRD/operator
- 외부 사내 IdP 연동

## 16. 미해결 확인 사항

- Kubernetes 1.36.1과 선택할 KubeVirt/CDI 버전의 실제 호환성
- 모든 worker 노드의 `/dev/kvm` 접근 가능 여부
- cluster-wide privileged workload 정책
- NFS CSI가 KubeVirt VM 디스크 용도로 충분한 성능과 access mode를 제공하는지
- KubeVirt console subresource를 백엔드에서 프록시할 때 사용할 정확한 프로토콜과 브라우저 콘솔 구현 방식
