import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Network, Plus, Trash2 } from "lucide-react";
import type { FormEvent } from "react";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api } from "../api";
import { projectPath, useProject } from "../app/Layout";
import { keys, useNetworks, useVMs } from "../app/queries";
import { fill } from "../format";
import { statusTone, useI18n } from "../i18n";
import { Alert, Badge, Button, Card, Empty, Field, IconButton, Input, Modal, PageHeader, Select, Spinner, useConfirm } from "../ui";

export function VPCDetail() {
  const { t, statusText, errorText } = useI18n();
  const { projectId, projectName } = useProject();
  const networkId = useParams().networkId ?? "";
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const networks = useNetworks(projectId);
  const vms = useVMs(projectId);
  const subnets = useQuery({
    queryKey: keys.subnets(projectId, networkId),
    queryFn: () => api.subnets(projectId, networkId),
  });
  const [creating, setCreating] = useState(false);

  const deleteVPC = useMutation({
    mutationFn: () => api.deleteNetwork(projectId, networkId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: keys.networks(projectId) });
      navigate(projectPath(projectId, "vpcs"));
    },
  });
  const deleteSubnet = useMutation({
    mutationFn: (subnetId: string) => api.deleteSubnet(projectId, networkId, subnetId),
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.subnets(projectId, networkId) }),
  });

  const network = networks.data?.find((item) => item.id === networkId);
  if (networks.isLoading) {
    return <Empty title={<Spinner label={t.loading} />} />;
  }
  if (!network) {
    return (
      <>
        <PageHeader crumbs={[{ label: t.navVPCs, to: projectPath(projectId, "vpcs") }]} title={networkId} />
        <Alert tone="error">{networks.isError ? errorText(networks.error) : t.vpcNotFound}</Alert>
      </>
    );
  }

  const subnetList = subnets.data ?? [];
  const subnetById = new Map(subnetList.map((subnet) => [subnet.id, subnet]));
  const attached = (vms.data ?? []).filter((vm) => vm.subnetId && subnetById.has(vm.subnetId));
  const publicCount = subnetList.filter((subnet) => subnet.type === "public").length;

  async function removeVPC() {
    const ok = await confirm({
      title: t.vpcDelete,
      message: fill(t.vpcDeleteConfirm, { name: network!.name }),
      confirmLabel: t.delete,
      cancelLabel: t.cancel,
      danger: true,
    });
    if (ok) {
      deleteVPC.mutate();
    }
  }

  async function removeSubnet(subnetId: string, name: string) {
    const ok = await confirm({
      title: fill(t.subnetDelete, { name }),
      message: fill(t.subnetDeleteConfirm, { name }),
      confirmLabel: t.delete,
      cancelLabel: t.cancel,
      danger: true,
    });
    if (ok) {
      deleteSubnet.mutate(subnetId);
    }
  }

  return (
    <>
      <PageHeader
        crumbs={[
          { label: projectName, to: projectPath(projectId) },
          { label: t.navVPCs, to: projectPath(projectId, "vpcs") },
          { label: network.name },
        ]}
        title={
          <span className="row">
            <Network size={22} aria-hidden /> {network.name}
          </span>
        }
        badge={
          <Badge tone={statusTone(network.status)} dot>
            {statusText(network.status)}
          </Badge>
        }
        subtitle={network.cidr ? `${network.cidr} · ${t.vpcIsolated}` : t.vpcLegacy}
        actions={
          <>
            <Button variant="primary" icon={Plus} onClick={() => setCreating(true)}>
              {t.subnetCreate}
            </Button>
            <Button variant="danger" icon={Trash2} loading={deleteVPC.isPending} onClick={removeVPC}>
              {t.vpcDelete}
            </Button>
          </>
        }
      />
      {deleteVPC.isError && <Alert tone="error">{errorText(deleteVPC.error)}</Alert>}

      <div className="resource-strip">
        <div>
          <span className="muted small">{t.vpcSubnets}</span>
          <strong>{subnetList.length}</strong>
        </div>
        <div>
          <span className="muted small">{t.subnetPublic}</span>
          <strong>{publicCount}</strong>
        </div>
        <div>
          <span className="muted small">{t.vpcAttachedVms}</span>
          <strong>{attached.length}</strong>
        </div>
      </div>

      <Card
        title={t.subnetTitle}
        description={network.cidr ? fill(t.subnetWithin, { cidr: network.cidr }) : undefined}
        flush
      >
        {deleteSubnet.isError && (
          <div style={{ padding: "12px 16px 0" }}>
            <Alert tone="error">{errorText(deleteSubnet.error)}</Alert>
          </div>
        )}
        {subnets.isLoading ? (
          <Empty title={<Spinner label={t.loading} />} />
        ) : subnetList.length === 0 ? (
          <Empty title={t.subnetEmpty} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t.subnetName}</th>
                  <th>{t.subnetType}</th>
                  <th>{t.vpcCidr}</th>
                  <th>{t.subnetGateway}</th>
                  <th>{t.subnetEgress}</th>
                  <th>{t.vpcVms}</th>
                  <th className="col-actions">
                    <span className="muted">{t.actions}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {subnetList.map((subnet) => (
                  <tr key={subnet.id}>
                    <td>
                      <strong>{subnet.name}</strong>
                    </td>
                    <td>
                      <Badge tone={subnet.type === "public" ? "info" : "neutral"}>
                        {subnet.type === "public" ? t.subnetPublic : t.subnetPrivate}
                      </Badge>
                    </td>
                    <td className="mono">{subnet.cidr}</td>
                    <td className="mono">{subnet.gateway}</td>
                    <td>{subnet.type === "public" ? t.subnetEgressPublic : t.subnetEgressPrivate}</td>
                    <td>{attached.filter((vm) => vm.subnetId === subnet.id).length}</td>
                    <td className="col-actions">
                      <IconButton
                        variant="danger"
                        icon={Trash2}
                        label={fill(t.subnetDelete, { name: subnet.name })}
                        disabled={deleteSubnet.isPending}
                        onClick={() => removeSubnet(subnet.id, subnet.name)}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {publicCount > 0 && (
          <div style={{ padding: 16 }}>
            <Alert tone="info">{t.subnetNatNote}</Alert>
          </div>
        )}
      </Card>

      <Card title={t.vpcAttachedVms} flush>
        {attached.length === 0 ? (
          <Empty title={t.vpcNoVms} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t.name}</th>
                  <th>{t.status}</th>
                  <th>{t.detailSubnet}</th>
                </tr>
              </thead>
              <tbody>
                {attached.map((vm) => (
                  <tr key={vm.id}>
                    <td>
                      <Link to={projectPath(projectId, `vms/${vm.id}`)}>{vm.name}</Link>
                    </td>
                    <td>
                      <Badge tone={statusTone(vm.status)} dot>
                        {statusText(vm.status)}
                      </Badge>
                    </td>
                    <td>{subnetById.get(vm.subnetId ?? "")?.name}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {creating && (
        <CreateSubnetDialog
          projectId={projectId}
          networkId={networkId}
          vpcCidr={network.cidr}
          onClose={() => setCreating(false)}
        />
      )}
    </>
  );
}

function CreateSubnetDialog({
  projectId,
  networkId,
  vpcCidr,
  onClose,
}: {
  projectId: string;
  networkId: string;
  vpcCidr: string;
  onClose: () => void;
}) {
  const { t, errorText } = useI18n();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [cidr, setCidr] = useState("");
  const [type, setType] = useState<"private" | "public">("private");
  const create = useMutation({
    mutationFn: () => api.createSubnet(projectId, networkId, { name: name.trim(), cidr: cidr.trim(), type }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: keys.subnets(projectId, networkId) });
      onClose();
    },
  });
  const valid = name.trim() !== "" && cidr.trim() !== "";

  function submit(event: FormEvent) {
    event.preventDefault();
    if (valid && !create.isPending) {
      create.mutate();
    }
  }

  return (
    <Modal
      title={t.subnetCreate}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t.cancel}</Button>
          <Button type="submit" form="subnet-form" variant="primary" disabled={!valid} loading={create.isPending}>
            {t.subnetCreate}
          </Button>
        </>
      }
    >
      <form id="subnet-form" onSubmit={submit} className="stack">
        <Field label={t.subnetName}>
          {(id) => <Input id={id} value={name} placeholder="pub" onChange={(event) => setName(event.target.value)} />}
        </Field>
        <Field label={t.vpcCidr} hint={vpcCidr ? fill(t.subnetWithin, { cidr: vpcCidr }) : undefined}>
          {(id) => <Input id={id} value={cidr} placeholder="10.40.1.0/24" onChange={(event) => setCidr(event.target.value)} />}
        </Field>
        <Field label={t.subnetType} hint={type === "public" ? t.subnetPublicHint : t.subnetPrivateHint}>
          {(id) => (
            <Select id={id} value={type} onChange={(event) => setType(event.target.value as "private" | "public")}>
              <option value="private">{t.subnetPrivate}</option>
              <option value="public">{t.subnetPublic}</option>
            </Select>
          )}
        </Field>
        {create.isError && <Alert tone="error">{errorText(create.error)}</Alert>}
      </form>
    </Modal>
  );
}
