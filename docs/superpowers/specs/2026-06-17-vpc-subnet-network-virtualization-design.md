# VPC/서브넷 네트워크 가상화 설계

작성일: 2026-06-17

## 1. 목표

Kloudia 포털에서 사용자가 **VPC와 서브넷을 직접 생성(생성 시 CIDR 지정)** 하고, 생성한 서브넷에 VM을 띄울 수 있는 네트워크 가상화 기능을 추가한다.

이 기능은 기존 설계 문서([2026-06-16-kubevirt-private-cloud-design.md](2026-06-16-kubevirt-private-cloud-design.md))에서 "후속 확장"(§15: Multus 기반 프로젝트별 사설 네트워크)으로 분류했던 항목을 제품 기능으로 구현하는 것이다.

## 2. 범위

### 포함

- VPC(가상 네트워크) 생성/조회/삭제 (생성 시 VPC CIDR을 사용자가 지정, 서브넷은 그 안에서만 생성)
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
| VPC CIDR | 사설 대역(RFC 1918) 안의 /16~/28, 클러스터 내부 대역과 겹치면 거부. 생성 후 변경 불가 |
| VPC–프로젝트 관계 | 프로젝트 1:N VPC, VPC 1:N 서브넷 |
| VM NIC | 서브넷 1개에 NIC 1개 |
| IP 할당 | 서브넷 CIDR에서 자동 할당 |
| 작업 범위 | 플랫폼 계층만 (kube-ovn+Multus 설치 가정) |

## 3. 전제 조건

- 클러스터에 **kube-ovn**이 설치되어 있어야 한다 (커스텀 VPC/Subnet CRD 제공, 보조 네트워크 모드).
- **Multus CNI**가 설치되어 있어야 한다 (`NetworkAttachmentDefinition` CRD).
- 기존 Cilium은 클러스터 기본 CNI로 유지한다 (교체하지 않음).
- kube-ovn은 보조 CNI 모드(Helm `cni_conf.NON_PRIMARY_CNI: true`, `CNI_CONFIG_PRIORITY`를 Multus·Cilium 뒤로)로 설치하고, NetworkPolicy·Service LB는 Cilium/kube-proxy에 맡긴다(`func.ENABLE_NP`, `func.ENABLE_LB` 끔). Cilium은 `cni-exclusive=false`여야 Multus 설정 파일이 남는다.
- **public 서브넷용 전제**: kube-ovn VPC NAT 게이트웨이 기능이 활성화되어 있고, egress용 외부 provider 네트워크(kube-ovn external network)가 인프라 계층에 구성되어 있어야 한다. private 서브넷만 사용할 경우에는 필요 없다.
  - 외부망은 `kube-system/ovn-vpc-external-network` NAD(노드 업링크 NIC 위 macvlan, IPAM은 kube-ovn)와 같은 이름의 kube-ovn Subnet(노드망 CIDR·게이트웨이, EIP로 쓸 범위 밖은 `excludeIps`)으로 구성한다. 이름은 `KLOUDIA_OVN_EXTERNAL_NETWORK`(기본 `ovn-vpc-external-network`)와 같아야 한다.
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
  cidr          TEXT,                   -- 사용자 지정 VPC CIDR (예: 10.10.0.0/16). VPC CIDR 도입 전 VPC는 NULL
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
    ID, ProjectID, Name, CIDR, Status string // CIDR: 도입 전 VPC는 빈 문자열
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
POST   /api/v1/projects/{projectID}/networks                          {name, cidr}
DELETE /api/v1/projects/{projectID}/networks/{networkID}

GET    /api/v1/projects/{projectID}/networks/{networkID}/subnets
POST   /api/v1/projects/{projectID}/networks/{networkID}/subnets      {name, cidr, type?, gateway?}
DELETE /api/v1/projects/{projectID}/networks/{networkID}/subnets/{subnetID}
```

- 응답 포맷은 기존과 동일하게 `{ data: ... }` 래핑.
- VM 생성 페이로드에 `subnetId` 추가. 네트워킹이 활성(kube-ovn 사용 가능)일 때 필수, 비활성일 때는 선택.

### 검증 규칙

- VPC `cidr`는 필수다. 사설 대역(10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16) 안의 /16~/28이어야 하며(`NETWORK_CIDR_INVALID`), 호스트 비트는 지워 저장한다(10.10.5.7/16 → 10.10.0.0/16).
- VPC `cidr`는 `KLOUDIA_RESERVED_CIDRS`의 대역과 겹칠 수 없다(`NETWORK_CIDR_RESERVED`). 기본값은 Cilium 파드(10.244.0.0/16)·서비스(10.96.0.0/12)·kube-ovn 기본/조인(10.16.0.0/16, 100.64.0.0/16) 대역이다. NAT 게이트웨이는 파드망(eth0)·서비스 경로와 VPC LAN을 함께 가지므로 겹치면 경로가 충돌한다. NAT 외부망으로 쓰는 노드망도 환경에 맞게 이 목록에 넣는다. 형식 검사(`NETWORK_CIDR_INVALID`)가 예약 대역 검사보다 먼저다.
- Kube-OVN `Vpc` CR에는 CIDR 필드가 없다. VPC CIDR은 KubeCloud가 서브넷 생성 시 거는 제약이다.
- 서브넷 `cidr`는 유효한 IPv4 CIDR이어야 한다 (`net/netip`로 파싱).
- 서브넷 `cidr`는 VPC CIDR 안에 있어야 한다(`SUBNET_CIDR_OUTSIDE_VPC`). VPC CIDR 도입 전에 만든 VPC(CIDR 없음)의 서브넷은 이 검사를 하지 않는다.
- 같은 VPC(`network_id`) 내 서브넷 CIDR는 서로 중첩될 수 없다.
- 서로 다른 VPC 간 CIDR 중첩은 허용한다.
- `gateway` 미지정 시 CIDR의 첫 사용 가능 IP를 기본값으로 한다. 지정 시 CIDR 범위 내여야 한다.
- `type`은 `private` 또는 `public`만 허용하며 미지정 시 `private`. `public` 요청 시 외부 egress 전제(VPC NAT 게이트웨이/외부망)가 미충족이면 `NETWORKING_UNAVAILABLE` 반환.
- VM 생성 시 `subnetId`는 같은 프로젝트에 속한 서브넷이어야 한다.

### 삭제 가드

- VM이 연결된 서브넷은 삭제 거부.
- VPC NAT 게이트웨이를 호스팅하는 public 서브넷(앵커)은 같은 VPC에 다른 public 서브넷이 남아 있으면 삭제 거부.
- 서브넷이 존재하는 VPC는 삭제 거부.

### 에러 코드 (기존 플랫폼 에러 매핑 패턴 따름)

- `NETWORK_CIDR_INVALID` (사설 대역 /16~/28이 아님)
- `NETWORK_CIDR_RESERVED` (클러스터 내부 대역과 겹침)
- `SUBNET_CIDR_INVALID`
- `SUBNET_CIDR_OUTSIDE_VPC`
- `SUBNET_CIDR_OVERLAP`
- `SUBNET_TYPE_INVALID` (private/public 외 값)
- `SUBNET_IN_USE`
- `SUBNET_HOSTS_NAT_GATEWAY` (다른 public 서브넷이 남아 있는데 NAT 게이트웨이 앵커 서브넷을 삭제하려 함)
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
  - NAT 게이트웨이는 VPC당 1개를 공유한다. LAN 쪽은 VPC에서 가장 먼저 만든 public 서브넷(앵커, `domain.NATGatewayAnchor`)에 붙고, `lanIp`는 앵커 CIDR의 마지막 사용 가능 주소(서브넷 게이트웨이와 겹치면 그 앞 주소)다.
  - kube-ovn이 보조 CNI(`NON_PRIMARY_CNI`)라 게이트웨이 파드의 eth0은 Cilium이 갖는다. 그래서 kube-ovn은 LAN 쪽을 스스로 붙이지 않으며, `VpcNatGateway`의 `metadata.annotations`에 `k8s.v1.cni.cncf.io/networks: <VM 네임스페이스>/<앵커 NAD>`를 넣어 LAN NIC를 붙인다.
  - egress 경로는 Vpc `spec.policyRoutes`로 건다. public 서브넷 CIDR마다 `{priority: 20000, match: "ip4.src == <CIDR>", action: reroute, nextHopIP: <lanIp>}` 정책을 둔다. kube-ovn이 서브넷마다 넣는 우선순위 31000 `ip4.dst == <서브넷> allow` 정책이 먼저 걸리므로 VPC 내부 트래픽은 게이트웨이로 가지 않는다. private 서브넷은 정책이 없어 외부로 나갈 경로가 없다.
    - VPC 전체 `0.0.0.0/0` 정적 경로는 쓰지 않는다. private 서브넷 트래픽까지 게이트웨이로 보내고, 게이트웨이는 SNAT 규칙이 없는 패킷도 원래 출발지 IP로 외부망에 내보낸다.
    - 출발지 기반 정적 경로(`policySrc`)도 쓰지 않는다. kube-ovn이 커스텀 VPC 서브넷마다 `src-ip <CIDR> → <서브넷 게이트웨이>` 정적 경로를 직접 관리해서, 같은 키의 경로를 덮어쓴다.
  - 서브넷 생성(public) → Subnet CR + NAD, (앵커면) `VpcNatGateway`, `IptablesEIP` + `IptablesSnatRule`(서브넷 CIDR을 EIP로 SNAT, 인바운드 없음)을 apply한 뒤 Vpc egress 정책을 public 서브넷 전체 기준으로 다시 쓴다. SNAT 규칙이 생긴 다음에 경로를 열어 SNAT 없이 나가는 트래픽이 없게 한다.
  - 서브넷 삭제(public) → Vpc egress 정책을 남은 public 서브넷 기준으로 먼저 다시 쓰고, SNAT 규칙·EIP를 지운다. 남은 public 서브넷이 없으면 `VpcNatGateway`도 지운다. 이어서 NAD delete + Subnet CR delete. 앵커는 마지막 public 서브넷일 때만 삭제되므로 게이트웨이는 그때만 사라진다.
  - VPC 삭제 → `VpcNatGateway` delete(남아 있을 경우 대비) 후 Vpc CR delete.
  - Vpc CR 변경은 전체 update가 아닌 merge patch로 한다. kube-ovn이 붙인 finalizer(`kubeovn.io/kube-ovn-controller`)와 annotation을 덮어쓰지 않기 위해서다.
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

- nested virtualization 환경(테스트 클러스터가 VM 위에서 실행)에서 kube-ovn Geneve 오버레이 MTU 적정값.
- 게이트웨이가 뜬 직후 약 10초 동안 앵커 서브넷에서만 egress가 실패했다가 회복된다(다른 public 서브넷은 즉시 동작). 게이트웨이 LAN 포트와 같은 서브넷이라 생기는 수렴 지연으로 보이나 원인은 확인하지 않았다.
- `lanIp`는 VM보다 먼저 만들어지는 게이트웨이가 차지하고 kube-ovn IPAM은 CIDR 앞쪽부터 할당하므로 충돌하지 않는다. 다만 `excludeIps`로 예약한 것은 아니다.
- 경로 정책은 요청 시점의 public 서브넷 목록으로 통째로 다시 쓴다. 같은 VPC에 public 서브넷을 동시에 만들거나 지우면 나중에 쓴 쪽이 다른 쪽 CIDR을 빠뜨릴 수 있다.
- 검증은 KubeVirt VM이 아닌, 같은 NAD를 붙인 파드로 했다. VM의 bridge 바인딩에서도 같은지는 확인이 필요하다.
