# 이미지 미리 가져오기 설계

작성일: 2026-11-07

## 1. 목표

이미지를 등록할 때 한 번 받아 클러스터에 보관하고(캐시 디스크), VM은 그 디스크를 복제해서 만든다.

- VM 생성이 외부 다운로드에 기대지 않는다. 이미지 서버가 느리거나 막혀도 이미 받아 둔 이미지로는 VM을 만든다.
- 이미지 URL을 받을 수 없으면(HTTP 오류, 접속 실패) **등록할 때** 400 `IMAGE_SOURCE_UNREACHABLE`로 거절한다. 헤더는 읽히지만 전체 가져오기가 실패하는 이미지는 캐시를 만들다 `Failed`가 된다.
- 이미지 상태(`Importing` / `Ready` / `Failed`)가 실제 의미를 갖는다.

## 2. 확인한 동작 (test-kv, KubeVirt 1.9.1, CDI 1.66.1, NFS 스토리지 클래스)

- 같은 네임스페이스 안의 DataVolume 복제(`source.pvc`)는 CDI의 호스트 복사 방식(`cloneType: copy`)으로 된다. CirrOS 기준 캐시 가져오기 16초, 복제 21초.
- 복제한 PVC를 원본보다 크게 만들어도 그 안의 `disk.img`는 원본 크기 그대로다. KubeVirt 1.9가 VM을 시작할 때 디스크 이미지를 PVC 크기까지 늘리므로, 게스트는 사양 크기의 디스크를 본다(3Gi PVC → 게스트 vda 2.82GiB).

## 3. 설계

### 캐시 디스크

- 이미지마다 DataVolume 하나: 이름 `image-<이미지 ID>`(63자를 넘으면 앞부분과 해시로 줄임), VM 네임스페이스(`KLOUDIA_KUBEVIRT_NAMESPACE`), 라벨 `kloudia.io/role=image-cache`. 이미지 ID와 소스 URL은 어노테이션(`kloudia.io/image-id`, `kloudia.io/source-url`)에 둔다. 라벨 값은 63자 제한이 있어 긴 ID를 담을 수 없다.
- 캐시는 VM이 직접 쓰지 않으므로, 사용 시점에 바인딩하는 스토리지 클래스(WaitForFirstConsumer)에서도 가져오기가 진행되도록 `cdi.kubevirt.io/storage.bind.immediate.requested: "true"`를 붙인다.
- 같은 네임스페이스에 두는 이유: 다른 네임스페이스에서 복제하면 CDI가 원본 네임스페이스의 `datavolumes/source` 권한을 따로 요구한다.
- 크기: 이미지의 최소 루트 디스크(`MinRootDiskBytes`)를 GiB 단위로 올림. 접근 모드와 스토리지 클래스는 VM 디스크와 같다. 복제본은 원본보다 작을 수 없으므로, 캐시가 Ready인 이미지로 VM을 만들 때는 사양의 루트 디스크가 이 올린 크기 이상이어야 한다.
- 크기를 모르는 이미지(qcow2가 아니거나 헤더를 읽지 못함)는 캐시를 만들지 않는다. 지금처럼 등록 즉시 `Ready`이고 VM마다 URL에서 받는다.

### 이미지 상태

| 상태 | 의미 | VM 생성 |
|---|---|---|
| `Importing` | 캐시를 받는 중 | 409 `IMAGE_NOT_READY` |
| `Ready` | 캐시가 있거나, 캐시 없이 URL로 받는 이미지 | 가능 |
| `Failed` | 캐시 가져오기가 반복해서 실패 (VM과 같은 판정: DataVolume `Error` 이벤트 또는 가져오기 오류 3회) | 409 `IMAGE_NOT_READY`, 이미지 목록에 원인 표시 |

- 새로 등록하는 이미지(크기를 아는 경우)는 `Importing`으로 시작한다.
- 이미 `Ready`인 기존 이미지는 그대로 `Ready`로 두고 캐시만 뒤에서 만든다. 캐시가 준비되기 전에 만든 VM은 URL에서 받는다.
- 이미지 테이블에 `cache_volume`(DataVolume 이름, 없으면 NULL), `cache_status`(`''`/`Importing`/`Ready`/`Failed`), `status_message`를 추가한다.

### 상태 갱신

- API 프로세스 안의 조정 루프가 15초마다 돈다(API는 레플리카 1개, 주기마다 2분 제한).
  - `Pending` → DataVolume을 만든다. 같은 이름의 DataVolume이 이미 있으면, 소스 URL이 같으면 이어서 보고 다르면(같은 이름으로 다시 등록된 이미지) 지우고 다음 주기에 새로 만든다. 4xx로 만들 수 없으면 `Failed`.
  - `Importing` → DataVolume 상태를 읽어 `Ready`/`Failed`로 바꾸고 이미지 상태도 함께 바꾼다. DataVolume에 걸린 이벤트는 현재 DataVolume의 UID와 같은 것만 본다. 이벤트는 한 시간쯤 남으므로, 지웠다가 같은 이름으로 다시 만든 이미지가 예전 오류로 실패하지 않게 하기 위해서다. DataVolume이 없으면 CDI가 완료된 DataVolume을 지운 경우일 수 있으므로, 볼륨이 바인딩돼 있으면 `Ready`, 아니면 다시 `Pending`.
  - `Ready` → 볼륨이 남아 있는지 확인한다. 없어졌으면 `Pending`으로 돌려 다시 가져오고, 그동안 VM은 URL에서 받는다.
- 상태 기록은 읽은 시점의 캐시 상태가 그대로일 때만 한다. 그사이 이미지가 지워졌으면 방금 만든 DataVolume을 지운다. 매 주기 끝에, 주인 이미지가 없는 캐시 DataVolume과 볼륨을 지운다.

### VM 생성

- 이미지 캐시가 `Ready`면 VM 루트 디스크 DataVolume의 `source`를 `pvc: {namespace, name: image-<ID>}`로 한다. 아니면 지금처럼 `http: {url}`.
- 디스크 크기는 사양의 루트 디스크 그대로다. 게스트는 KubeVirt가 늘린 크기를 본다.

### 이미지 삭제

- 캐시 DataVolume과 볼륨(PVC)을 함께 지운다. CDI가 완료된 DataVolume을 지운 클러스터에서는 볼륨만 남아 있을 수 있기 때문이다. 요청이 끊겨도 지우도록 요청 컨텍스트와 분리해 실행하고, 실패해도 조정 루프가 주인 없는 캐시로 정리한다.
- 이미지를 쓰는 VM이 있으면 지금처럼 거절한다. 캐시에서 이미 복제한 VM 디스크는 독립된 PVC라 영향이 없다.
- RBAC: 복제에 필요한 `datavolumes/source` create, 볼륨 삭제에 필요한 `persistentvolumeclaims` delete.

## 4. 테스트

- 단위: 캐시 DataVolume 매니페스트(이름, 라벨, 크기 올림, 접근 모드), VM 매니페스트의 source 선택(캐시 Ready → pvc, 아니면 http), 조정 루프의 상태 전이(가짜 클러스터 상태).
- DB: 상태·캐시 컬럼 마이그레이션, 새 이미지의 `Importing` 시작, 크기 모르는 이미지의 즉시 `Ready`.
- e2e: 존재하지 않는 URL 등록 거절(400), 이미지 등록 → `Importing`(이때 VM 생성 409) → `Ready`, 그 이미지로 VM 생성 시 루트 디스크가 캐시에서 복제되는지(DataVolume source), 게스트 디스크가 사양 크기인지, 이미지 삭제 시 캐시도 지워지는지.
