import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Network, Plus, Search } from "lucide-react";
import type { FormEvent } from "react";
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api } from "../api";
import { projectPath, useProject } from "../app/Layout";
import { keys, useAllSubnets, useNetworks, useVMs } from "../app/queries";
import { fill, formatDateTime } from "../format";
import { statusTone, useI18n } from "../i18n";
import { Alert, Badge, Button, Card, Empty, Field, Input, PageHeader, Spinner } from "../ui";

export function VPCList() {
  const { t, language, statusText, errorText } = useI18n();
  const { projectId, projectName } = useProject();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const networks = useNetworks(projectId);
  const subnets = useAllSubnets(projectId, networks.data);
  const vms = useVMs(projectId);
  const [search, setSearch] = useState("");
  const [name, setName] = useState("");
  const [cidr, setCidr] = useState("");

  const create = useMutation({
    mutationFn: () => api.createNetwork(projectId, name.trim(), cidr.trim()),
    onSuccess: (network) => {
      setName("");
      setCidr("");
      void queryClient.invalidateQueries({ queryKey: keys.networks(projectId) });
      navigate(projectPath(projectId, `vpcs/${network.id}`));
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (name.trim() && cidr.trim()) {
      create.mutate();
    }
  }

  const query = search.trim().toLowerCase();
  const all = networks.data ?? [];
  const visible = all.filter((network) => !query || `${network.name} ${network.cidr}`.toLowerCase().includes(query));
  const vmCount = (networkId: string) =>
    (vms.data ?? []).filter((vm) => vm.subnetId && subnets.byId.get(vm.subnetId)?.networkId === networkId).length;

  return (
    <>
      <PageHeader
        crumbs={[{ label: projectName, to: projectPath(projectId) }, { label: t.navVPCs }]}
        title={t.vpcTitle}
        subtitle={t.vpcSubtitle}
      />
      <Card flush>
        <div className="toolbar">
          <div className="toolbar-search row" style={{ flexWrap: "nowrap" }}>
            <Search size={16} className="muted" aria-hidden />
            <Input type="search" aria-label={t.vpcSearch} placeholder={t.vpcSearch} value={search} onChange={(event) => setSearch(event.target.value)} />
          </div>
          <span className="toolbar-spacer" />
          <span className="muted small">{fill(t.totalCount, { n: all.length })}</span>
        </div>
        {networks.isLoading ? (
          <Empty title={<Spinner label={t.loading} />} />
        ) : networks.isError ? (
          <div style={{ padding: 16 }}>
            <Alert tone="error">{errorText(networks.error)}</Alert>
          </div>
        ) : all.length === 0 ? (
          <Empty icon={Network} title={t.vpcEmpty}>
            {t.vpcEmptyHint}
          </Empty>
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t.name}</th>
                  <th>{t.vpcCidr}</th>
                  <th>{t.vpcSubnets}</th>
                  <th>{t.vpcVms}</th>
                  <th>{t.status}</th>
                  <th>{t.created}</th>
                </tr>
              </thead>
              <tbody>
                {visible.map((network) => (
                  <tr key={network.id}>
                    <td>
                      <Link to={projectPath(projectId, `vpcs/${network.id}`)}>{network.name}</Link>
                    </td>
                    <td className="mono">{network.cidr || "—"}</td>
                    <td>{subnets.byNetwork.get(network.id)?.length ?? "–"}</td>
                    <td>{vmCount(network.id)}</td>
                    <td>
                      <Badge tone={statusTone(network.status)} dot>
                        {statusText(network.status)}
                      </Badge>
                    </td>
                    <td className="muted">{formatDateTime(network.createdAt, language)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Card title={t.vpcCreate} description={t.vpcCreateNote}>
        <form onSubmit={submit} className="stack">
          <div className="form-grid">
            <Field label={t.vpcNameLabel}>
              {(id) => <Input id={id} value={name} placeholder={t.vpcNamePlaceholder} onChange={(event) => setName(event.target.value)} />}
            </Field>
            <Field label={t.vpcCidrLabel} hint={t.vpcCidrHint}>
              {(id) => <Input id={id} value={cidr} placeholder={t.vpcCidrPlaceholder} onChange={(event) => setCidr(event.target.value)} />}
            </Field>
          </div>
          {create.isError && <Alert tone="error">{errorText(create.error)}</Alert>}
          <div className="form-actions">
            <Button type="submit" variant="primary" icon={Plus} disabled={!name.trim() || !cidr.trim()} loading={create.isPending}>
              {t.vpcCreate}
            </Button>
          </div>
        </form>
      </Card>
    </>
  );
}
