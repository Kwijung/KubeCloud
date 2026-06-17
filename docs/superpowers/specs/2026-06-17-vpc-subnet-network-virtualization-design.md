# VPC/서브넷 네트워크 가상화 설계

작성일: 2026-06-17

## 1. 목표

Kloudia 포털에서 사용자가 **VPC와 서브넷을 직접 생성(생성 시 CIDR 지정)** 하고, 생성한 서브넷에 VM을 띄울 수 있는 네트워크 가상화 기능을 추가한다.

이 기능은 기존 설계 문서([2026-06-16-kubevirt-private-cloud-design.md](2026-06-16-kubevirt-private-cloud-design.md))에서 "후속 확장"(§15: Multus 기반 프로젝트별 사설 네트워크)으로 분류했던 항목을 제품 기능으로 구현하는 것이다.

## 2. 범위

### 포함

- VPC(가상 네트워크) 생성/조회/삭제
- 서브넷 생성/조회/삭제 (생성 시 CIDR를 사용자가 지정)
- 서브넷 타입 선택: **private / public**
  - private: 외부와 단절 (외부 통신 불가, 기본값)
  - public: 아웃바운드 egress 허용 (VM → 외부 인터넷, SNAT)
- VM 생성 시 서브넷 선택 → 해당 서브넷 CIDR에서 IP 자동 할당
- 같은 VPC 내 서브넷 CIDR 중첩 금지 검증
- 서로 다른 VPC 간 CIDR 중첩 허용 (테넌트 격리, AWS VPC 동일 모델)
- VPC/서브넷 삭제 가드 (사용 중이면 거부)
- kube-ovn / Multus 설치 여부 preflight 체크

### 제외 (이번 범위 아님)

- 인바운드 외부 접근: 외부 IP(EIP) / 포트포워딩 / DNAT / 방화벽 정책
- VM당 다중 NIC / 다중 서브넷 연결
- 정적 IP 지정 (IP는 서브넷 CIDR에서 자동 할당만)
- VPC 간 피어링 / 라우팅
- kube-ovn / Multus / VPC NAT 게이트웨이 외부망 설치 스크립트 (인프라 계층, 별도 작업으로 분리)

### 결정사항 요약

| 항목 | 결정 |
|------|------|
| 연결 범위 | private는 내부 격리, public은 아웃바운드 egress(SNAT) 허용 |
| 서브넷 타입 | private(기본) / public, 생성 시 선택 |
| 격리 모델 | CIDR 중첩 허용 → 커스텀 VPC |
| VPC–프로젝트 관계 | 프로젝트 1:N VPC, VPC 1:N 서브넷 |
| VM NIC | 서브넷 1개에 NIC 1개 |
| IP 할당 | 서브넷 CIDR에서 자동 할당 |
| 작업 범위 | 플랫폼 계층만 (kube-ovn+Multus 설치 가정) |

## 3. 전제 조건

- 클러스터에 **kube-ovn**이 설치되어 있어야 한다 (커스텀 VPC/Subnet CRD 제공, 보조 네트워크 모드).
- **Multus CNI**가 설치되어 있어야 한다 (`NetworkAttachmentDefinition` CRD).
- 기존 Cilium은 클러스터 기본 CNI로 유지한다 (교체하지 않음).
- **public 서브넷용 전제**: kube-ovn VPC NAT 게이트웨이 기능이 활성화되어 있고, egress용 외부 provider 네트워크(kube-ovn external network)가 인프라 계층에 구성되어 있어야 한다. private 서브넷만 사용할 경우에는 필요 없다.
- 이 전제는 preflight 체크로 검증하며, 미충족 시 네트워크 기능(또는 public 서브넷 생성)은 명확한 에러를 반환한다.

## 4. 아키텍처 / 인프라 매핑

기존 Cilium을 클러스터 기본 CNI로 유지하고, kube-ovn 커스텀 VPC + Subnet을 Multus 보조 네트워크로 VM에 연결한다.

```text
[React Portal] --HTTP--> [Go Backend API]
                              |
                              | client-go dynamic client
                              v
                    [Kubernetes + kube-ovn + Multus]
   VPC      -> kube-ovn Vpc CR        (cluster-scoped, 격리 가상 라우터)
   Subnet   -> kube-ovn Subnet CR     (cluster-scoped, cidrBlock/gateway/vpc/provider)
   연결매개  -> Multus NAD             (VM 네임스페이스, subnet provider 참조)
   VM       -> KubeVirt VirtualMachine (게스트 NIC 1개 = NAD/서브넷, bridge 바인딩)
```

- VPC → kube-ovn `Vpc` CR. cluster-scoped, 격리된 가상 라우터이므로 VPC 간 CIDR 중첩이 허용된다.
- Subnet → kube-ovn `Subnet` CR. `vpc`(소속 VPC), `cidrBlock`, `gateway`, `protocol: IPv4`, `provider`(NAD 연결자) 지정. cluster-scoped.
- 각 Subnet마다 VM 네임스페이스(`KLOUDIA_KUBEVIRT_NAMESPACE`, 기본 `kloudia-vms`)에 `NetworkAttachmentDefinition`을 생성하고, kube-ovn Subnet의 `provider`가 이 NAD를 가리키게 한다.
- VM은 virt-launcher 파드가 Cilium 기본망을 유지(파드 스케줄링 요건)하되, **게스트 VM에는 pod network를 노출하지 않고** 해당 NAD(=VPC 서브넷)만 게스트 NIC로 바인딩한다(`bridge`). 결과적으로 게스트 OS는 서브넷 CIDR의 IP를 가진 NIC 1개만 보게 된다.
- IP는 kube-ovn IPAM이 서브넷 CIDR에서 자동 할당한다.
- **서브넷 타입에 따른 외부 통신**:
  - private 서브넷: 외부 egress 경로 없음. 같은 VPC 내 통신만 가능.
  - public 서브넷: 커스텀 VPC는 기본적으로 외부와 단절되므로, 해당 VPC에 kube-ovn `VpcNatGateway`(외부 provider 네트워크 연결)를 두고 서브넷 CIDR에 대한 SNAT 규칙으로 아웃바운드 egress를 제공한다. 인바운드(외부→VM) 경로는 만들지 않는다.
- 콘솔은 KubeVirt subresource(API 서버 경유 virt-launcher 연결)이므로 게스트 네트워크 구성과 무관하게 기존대로 동작한다.

## 5. 데이터 모델 (PostgreSQL)

신규 테이블 2개와 `virtual_machines` 컬럼 1개를 추가한다.

```sql
CREATE TABLE networks (
  id            TEXT PRIMARY KEY,
  project_id    TEXT NOT NULL REFERENCES projects(id),
  name          TEXT NOT NULL,
  ovn_vpc_name  TEXT NOT NULL,          -- kube-ovn Vpc CR 이름 (예: kloudia-vpc-<id>)
  status        TEXT NOT NULL,          -- Provisioning / Ready / Failed
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (project_id, name)
);

CREATE TABLE subnets (
  id               TEXT PRIMARY KEY,
  network_id       TEXT NOT NULL REFERENCES networks(id),
  project_id       TEXT NOT NULL REFERENCES projects(id),
  name             TEXT NOT NULL,
  cidr             TEXT NOT NULL,        -- 사용자 지정 (예: 10.0.0.0/24)
  gateway          TEXT NOT NULL,        -- 미지정 시 CIDR 첫 사용 IP
  type             TEXT NOT NULL,        -- private | public (기본 private)
  ovn_subnet_name  TEXT NOT NULL,        -- kube-ovn Subnet CR 이름
  nad_name         TEXT NOT NULL,        -- Multus NAD 이름
  status           TEXT NOT NULL,        -- Provisioning / Ready / Failed
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (network_id, name)
);

ALTER TABLE virtual_machines
  ADD COLUMN subnet_id TEXT REFERENCES subnets(id);  -- nullable: 기존 VM/네트워킹 비활성 호환
```

도메인 타입 추가:

```go
type Network struct {
    ID, ProjectID, Name, Status string
    CreatedAt time.Time
}
type Subnet struct {
    ID, NetworkID, ProjectID, Name, CIDR, Gateway, Type, Status string // Type: private|public
    CreatedAt time.Time
}
```

- `CreateVirtualMachineInput`에 `SubnetID` 추가.
- `VirtualMachineProvisioningPlan`에 `Subnet *Subnet` (및 NAD/서브넷 이름) 추가.
- `VirtualMachine`/`VirtualMachineDetail`에 `SubnetID`, IP 표시용 필드 활용.

## 6. API (chi 라우터)

```
GET    /api/v1/projects/{projectID}/networks
POST   /api/v1/projects/{projectID}/networks                          {name}
DELETE /api/v1/projects/{projectID}/networks/{networkID}

GET    /api/v1/projects/{projectID}/networks/{networkID}/subnets
POST   /api/v1/projects/{projectID}/networks/{networkID}/subnets      {name, cidr, type?, gateway?}
DELETE /api/v1/projects/{projectID}/networks/{networkID}/subnets/{subnetID}
```

- 응답 포맷은 기존과 동일하게 `{ data: ... }` 래핑.
- VM 생성 페이로드에 `subnetId` 추가. 네트워킹이 활성(kube-ovn 사용 가능)일 때 필수, 비활성일 때는 선택.

### 검증 규칙

- `cidr`는 유효한 IPv4 CIDR이어야 한다 (`net/netip`로 파싱).
- 같은 VPC(`network_id`) 내 서브넷 CIDR는 서로 중첩될 수 없다.
- 서로 다른 VPC 간 CIDR 중첩은 허용한다.
- `gateway` 미지정 시 CIDR의 첫 사용 가능 IP를 기본값으로 한다. 지정 시 CIDR 범위 내여야 한다.
- `type`은 `private` 또는 `public`만 허용하며 미지정 시 `private`. `public` 요청 시 외부 egress 전제(VPC NAT 게이트웨이/외부망)가 미충족이면 `NETWORKING_UNAVAILABLE` 반환.
- VM 생성 시 `subnetId`는 같은 프로젝트에 속한 서브넷이어야 한다.

### 삭제 가드

- VM이 연결된 서브넷은 삭제 거부.
- 서브넷이 존재하는 VPC는 삭제 거부.

### 에러 코드 (기존 플랫폼 에러 매핑 패턴 따름)

- `SUBNET_CIDR_INVALID`
- `SUBNET_CIDR_OVERLAP`
- `SUBNET_TYPE_INVALID` (private/public 외 값)
- `SUBNET_IN_USE`
- `NETWORK_IN_USE`
- `NETWORKING_UNAVAILABLE` (kube-ovn/Multus 미설치 또는 비활성)

## 7. 프로비저닝 흐름 (kubevirt 패키지)

- `dynamic_applier.go`의 `resourceMapping()`에 GVR 추가:
  - `vpcs` (kubeovn.io/v1, cluster-scoped)
  - `subnets` (kubeovn.io/v1, cluster-scoped)
  - `network-attachment-definitions` (k8s.cni.cncf.io/v1, namespaced)
  - public 서브넷 egress용: `vpc-nat-gateways`, `iptables-eips`, `iptables-snat-rules` (kubeovn.io/v1)
- 신규 `network_manifest.go`: Vpc CR / Subnet CR / NAD / VpcNatGateway / SNAT 관련 매니페스트 생성 함수.
- 신규 프로비저너 동작:
  - VPC 생성 → Vpc CR apply.
  - 서브넷 생성(private) → Subnet CR apply + NAD apply (VM 네임스페이스).
  - 서브넷 생성(public) → 위에 더해 VPC에 `VpcNatGateway`(외부망 연결)가 없으면 생성하고, 해당 서브넷 CIDR에 대한 SNAT egress 규칙(`IptablesEIP` + `IptablesSnatRule`)을 apply. 게이트웨이의 외부 IP로 SNAT만 수행(인바운드 없음).
  - 서브넷 삭제 → (public이면 SNAT 규칙/EIP delete 후) NAD delete + Subnet CR delete.
  - VPC 삭제 → (VpcNatGateway 있으면 delete 후) Vpc CR delete.
- `manifest.go BuildResources`: plan에 Subnet이 있으면 VM template에 다음을 주입한다.
  - `spec.template.spec.networks`: multus 네트워크 1개 (`multus.networkName: <ns>/<nad>`), pod network 미포함.
  - `spec.template.spec.domain.devices.interfaces`: 해당 네트워크에 `bridge` 바인딩 1개.
  - Subnet이 없으면(레거시/비활성) 기존 동작(기본 pod network) 유지.
- 비활성 모드(`KLOUDIA_KUBEVIRT_ENABLED=false`)에서는 기존 VM 흐름과 동일하게 DB 레코드만 기록(noop applier)한다.

## 8. 상태 조회 (status_reader)

- VMI IP 추출 시 multus 보조 인터페이스의 IP를 우선 사용하도록 보정한다.
- 서브넷 상태: kube-ovn `Subnet` CR의 `.status`(가용 IP 수, conditions)를 선택적으로 노출.
- `GetPreflight()`에 체크 추가:
  - kube-ovn CRD(`vpcs.kubeovn.io`, `subnets.kubeovn.io`) 존재 여부
  - Multus CRD(`network-attachment-definitions.k8s.cni.cncf.io`) 존재 여부
  - public egress 가용성: VPC NAT 게이트웨이 CRD(`vpc-nat-gateways.kubeovn.io`) 및 외부 provider 네트워크 구성 여부 (미충족 시 Warning, public 서브넷 생성만 차단)

## 9. 프론트엔드

- **Networks 패널 신설** (App.tsx):
  - VPC 목록 + 생성 폼(name).
  - 선택한 VPC의 서브넷 목록 + 생성 폼(name, CIDR, type[private/public], gateway 선택). 목록에 타입 배지 표시.
  - 삭제 액션 (가드 위반 시 에러 표시).
- **VM 생성 폼**: 서브넷 드롭다운 추가 (선택 프로젝트의 서브넷으로 필터).
- **VM 상세**: 연결 서브넷명, 타입(private/public), 할당 IP 표시.
- api.ts: networks/subnets 엔드포인트 클라이언트 + 타입 추가.
- i18n.ts: ko/en 메시지 키 추가 (네트워크/서브넷/CIDR/에러 등).

## 10. 설정

- 신규 환경변수 없음. kube-ovn/Multus 연동은 기존 KUBECONFIG/dynamic client를 재사용한다.
- 기존 `KLOUDIA_KUBEVIRT_ENABLED`로 네트워킹 적용 여부를 함께 게이트한다 (false면 DB 메타데이터만).
- NAD 생성 네임스페이스는 기존 `KLOUDIA_KUBEVIRT_NAMESPACE`를 사용한다.

## 11. 테스트 전략

### 단위 테스트

- store: networks/subnets CRUD, CIDR 중첩 검증, 삭제 가드, 서브넷 type 저장.
- httpapi: VPC/서브넷 생성·검증(잘못된 CIDR, 중첩, 잘못된 type)·삭제 가드, `subnetId` 포함 VM 생성 validation.
- kubevirt: Vpc/Subnet/NAD 매니페스트 생성 골든 테스트, subnet 포함 VM 매니페스트(네트워크/인터페이스 블록) 골든 테스트, public 서브넷의 VpcNatGateway/SNAT 매니페스트 골든 테스트.

### 통합 테스트

- 실제 kube-ovn 동작은 VM 기반 테스트 클러스터에서 수동/nightly로 검증 (기존 스펙 §14 테스트 전략과 동일). kind/minikube는 KubeVirt+kube-ovn 조건이 제한적이므로 기준 환경에서 제외.

## 12. 미해결 확인 사항

- kube-ovn 커스텀 VPC 서브넷을 Multus 보조 네트워크로 KubeVirt VM에 붙일 때의 정확한 NAD 설정과 `provider` 명명 규칙 (테스트 클러스터에서 1회 검증 필요).
- nested virtualization 환경(테스트 클러스터가 VM 위에서 실행)에서 kube-ovn Geneve 오버레이 MTU 적정값.
- kube-ovn 설치 모드 (primary CNI 동거 vs secondary network 전용)에 따른 Subnet `provider`/`vpc` 필드 동작 차이 — 인프라 작업에서 확정.
- 커스텀 VPC에서 public 서브넷 egress를 위한 정확한 kube-ovn 리소스 체인(`VpcNatGateway` + `IptablesEIP` + `IptablesSnatRule`)과 외부 provider 네트워크 명명/연결 방식 — 테스트 클러스터에서 1회 검증 필요. VPC당 NAT 게이트웨이 1개 공유 vs 서브넷별 분리 여부도 이때 확정.
