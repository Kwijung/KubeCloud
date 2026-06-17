# Kloudia

KubeVirt-backed private cloud platform.

## Current Status

This repository contains:

- Infra spike scripts for KubeVirt/CDI validation.
- Portal MVP scaffold with project, flavor, image, and VM inventory APIs.

## Local Development

Start PostgreSQL:

```bash
docker compose up -d postgres
```

Run the backend:

```bash
cd backend
go run ./cmd/api
```

Run the frontend:

```bash
cd frontend
npm install
npm run dev
```

Health checks:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/api/v1/health
```

Catalog APIs:

```bash
curl http://localhost:8080/api/v1/projects
curl http://localhost:8080/api/v1/flavors
curl http://localhost:8080/api/v1/projects/platform/images
curl http://localhost:8080/api/v1/projects/platform/vms
curl http://localhost:8080/api/v1/projects/platform/vms/platform-api-01
curl -X POST http://localhost:8080/api/v1/projects/platform/vms \
  -H 'Content-Type: application/json' \
  -d '{"name":"api-01","imageId":"platform-cirros","flavorId":"small"}'
curl -X DELETE http://localhost:8080/api/v1/projects/platform/vms/platform-api-01
```

The VM create endpoint persists a VM record and prepares the KubeVirt/CDI resource
model. Set `KLOUDIA_KUBEVIRT_ENABLED=true` to apply generated `DataVolume` and
`VirtualMachine` resources to Kubernetes. The delete endpoint removes KubeVirt
resources first and then deletes the database record.

KubeVirt provisioning settings:

```bash
KLOUDIA_KUBEVIRT_ENABLED=true
KUBECONFIG=/home/cloudia/.kube/config
KLOUDIA_KUBEVIRT_NAMESPACE=kloudia-vms
KLOUDIA_KUBEVIRT_STORAGE_CLASS=test-delete
```

The default seeded `small` flavor is sized for the current VM-based test cluster:
`1 vCPU`, `512Mi` memory, and `2Gi` root disk.

Run tests:

```bash
make backend-test
make frontend-build
```

Run the Postgres-backed store integration test:

```bash
cd backend
KLOUDIA_STORE_TEST_DATABASE_URL='postgres://kloudia:kloudia@localhost:5432/kloudia?sslmode=disable' go test ./internal/store
```

## Test Cluster Development

The current test cluster runs on `cloudia@192.168.160.81`. Dev-only cluster
manifests are in `deploy/dev`:

```bash
kubectl apply -f deploy/dev/kloudia-namespaces.yaml
kubectl apply -f deploy/dev/postgres.yaml
```

On the control-plane VM, the backend can run against the cluster Postgres through
a local port-forward:

```bash
kubectl -n kloudia-system port-forward svc/kloudia-postgres 15432:5432
DATABASE_URL='postgres://kloudia:kloudia@127.0.0.1:15432/kloudia?sslmode=disable' \
KLOUDIA_API_ADDR='0.0.0.0:8080' \
KLOUDIA_KUBEVIRT_ENABLED=true \
KUBECONFIG=/home/cloudia/.kube/config \
KLOUDIA_KUBEVIRT_NAMESPACE=kloudia-vms \
KLOUDIA_KUBEVIRT_STORAGE_CLASS=test-delete \
./kloudia-api
```

The API creates real KubeVirt/CDI resources, but VM status is not yet reconciled
back from Kubernetes into the database.

## Infra Spike

See [docs/infra/kubevirt-infra-spike.md](docs/infra/kubevirt-infra-spike.md).
