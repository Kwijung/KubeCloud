import { useQueryClient } from "@tanstack/react-query";
import { Play, Plus, RefreshCw, Search, Server, Square, Trash2 } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api } from "../api";
import { projectPath, useProject } from "../app/Layout";
import { flavorMap, keys, LIST_REFRESH_MS, useAllSubnets, useFlavors, useImages, useNetworks, useVMs } from "../app/queries";
import { fill, formatBytes, formatDateTime } from "../format";
import { statusTone, useI18n } from "../i18n";
import { Alert, Badge, Button, Card, Empty, IconButton, Input, PageHeader, Select, Spinner, useConfirm } from "../ui";

type BulkAction = "start" | "stop" | "delete";

export function VMList() {
  const { t, language, statusText, errorText } = useI18n();
  const { projectId, projectName } = useProject();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const queryClient = useQueryClient();
  const vms = useVMs(projectId);
  const images = useImages(projectId);
  const networks = useNetworks(projectId);
  const subnets = useAllSubnets(projectId, networks.data);
  const flavors = flavorMap(useFlavors().data);
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("");
  const [vpcFilter, setVpcFilter] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState<BulkAction | null>(null);
  const [bulkError, setBulkError] = useState("");

  const imageNames = new Map((images.data ?? []).map((image) => [image.id, image.name]));
  const networkNames = new Map((networks.data ?? []).map((network) => [network.id, network.name]));
  const all = vms.data ?? [];
  const statuses = [...new Set(all.map((vm) => vm.status))].sort();
  const query = search.trim().toLowerCase();
  const visible = all.filter((vm) => {
    if (statusFilter && vm.status !== statusFilter) {
      return false;
    }
    const subnet = vm.subnetId ? subnets.byId.get(vm.subnetId) : undefined;
    if (vpcFilter === "-" ? Boolean(vm.subnetId) : vpcFilter && subnet?.networkId !== vpcFilter) {
      return false;
    }
    return (
      !query ||
      [vm.name, vm.id, imageNames.get(vm.imageId) ?? vm.imageId].some((value) => value.toLowerCase().includes(query))
    );
  });
  // Selection only counts VMs that are still listed and visible.
  const selectedVMs = visible.filter((vm) => selected.has(vm.id));
  const allSelected = visible.length > 0 && selectedVMs.length === visible.length;

  function toggle(id: string) {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }

  async function runBulk(action: BulkAction) {
    const targets = selectedVMs;
    if (targets.length === 0) {
      return;
    }
    const names = targets.map((vm) => vm.name).join(", ");
    if (action !== "start") {
      const ok = await confirm({
        title: action === "delete" ? t.vmDeleteConfirmTitle : t.vmStopConfirmTitle,
        message: fill(action === "delete" ? t.vmDeleteConfirm : t.vmStopConfirm, { names }),
        confirmLabel: action === "delete" ? t.vmDelete : t.vmStop,
        cancelLabel: t.cancel,
        danger: true,
      });
      if (!ok) {
        return;
      }
    }
    setBusy(action);
    setBulkError("");
    const call = action === "start" ? api.startVM : action === "stop" ? api.stopVM : api.deleteVM;
    const results = await Promise.allSettled(targets.map((vm) => call(projectId, vm.id)));
    const failures = results.filter((result): result is PromiseRejectedResult => result.status === "rejected");
    if (failures.length > 0) {
      setBulkError(fill(t.vmBulkFailed, { n: failures.length, reason: errorText(failures[0].reason) }));
    }
    if (action === "delete") {
      setSelected(new Set());
    }
    setBusy(null);
    await queryClient.invalidateQueries({ queryKey: keys.vms(projectId) });
  }

  function networkLabel(subnetId: string | undefined) {
    if (!subnetId) {
      return <span className="muted">{t.vmDefaultNetwork}</span>;
    }
    const subnet = subnets.byId.get(subnetId);
    if (!subnet) {
      return <span className="mono">{subnetId}</span>;
    }
    return (
      <Link to={projectPath(projectId, `vpcs/${subnet.networkId}`)}>
        {networkNames.get(subnet.networkId) ?? subnet.networkId} / {subnet.name}
      </Link>
    );
  }

  return (
    <>
      <PageHeader
        crumbs={[{ label: projectName, to: projectPath(projectId) }, { label: t.navVMs }]}
        title={t.vmTitle}
        subtitle={fill(t.vmSubtitle, { project: projectName })}
        actions={
          <Button variant="primary" icon={Plus} onClick={() => navigate(projectPath(projectId, "vms/new"))}>
            {t.vmCreate}
          </Button>
        }
      />
      {bulkError && <Alert tone="error">{bulkError}</Alert>}
      <Card flush>
        <div className="toolbar">
          <div className="toolbar-search row" style={{ flexWrap: "nowrap" }}>
            <Search size={16} className="muted" aria-hidden />
            <Input
              type="search"
              aria-label={t.vmSearch}
              placeholder={t.vmSearch}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </div>
          <Select
            className="input select toolbar-filter"
            aria-label={t.vmFilterStatus}
            value={statusFilter}
            onChange={(event) => setStatusFilter(event.target.value)}
          >
            <option value="">
              {t.vmFilterStatus}: {t.all}
            </option>
            {statuses.map((status) => (
              <option key={status} value={status}>
                {statusText(status)}
              </option>
            ))}
          </Select>
          <Select
            className="input select toolbar-filter"
            aria-label={t.vmFilterVpc}
            value={vpcFilter}
            onChange={(event) => setVpcFilter(event.target.value)}
          >
            <option value="">
              {t.vmFilterVpc}: {t.all}
            </option>
            <option value="-">{t.vmDefaultNetwork}</option>
            {(networks.data ?? []).map((network) => (
              <option key={network.id} value={network.id}>
                {network.name}
              </option>
            ))}
          </Select>
          <span className="toolbar-spacer" />
          {selectedVMs.length > 0 && <span className="muted small">{fill(t.selectedCount, { n: selectedVMs.length })}</span>}
          <Button size="sm" icon={Play} disabled={selectedVMs.length === 0} loading={busy === "start"} onClick={() => runBulk("start")}>
            {t.vmStart}
          </Button>
          <Button size="sm" icon={Square} disabled={selectedVMs.length === 0} loading={busy === "stop"} onClick={() => runBulk("stop")}>
            {t.vmStop}
          </Button>
          <Button
            size="sm"
            variant="danger"
            icon={Trash2}
            disabled={selectedVMs.length === 0}
            loading={busy === "delete"}
            onClick={() => runBulk("delete")}
          >
            {t.vmDelete}
          </Button>
          <IconButton label={t.refresh} icon={RefreshCw} onClick={() => vms.refetch()} />
        </div>
        {vms.isLoading ? (
          <Empty title={<Spinner label={t.loading} />} />
        ) : vms.isError ? (
          <div style={{ padding: 16 }}>
            <Alert tone="error">{errorText(vms.error)}</Alert>
          </div>
        ) : all.length === 0 ? (
          <Empty icon={Server} title={t.vmEmpty}>
            {t.vmEmptyHint}
          </Empty>
        ) : visible.length === 0 ? (
          <Empty title={t.vmNoMatch} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th className="col-check">
                    <input
                      type="checkbox"
                      aria-label={t.vmSelectAll}
                      checked={allSelected}
                      onChange={() => setSelected(allSelected ? new Set() : new Set(visible.map((vm) => vm.id)))}
                    />
                  </th>
                  <th>{t.name}</th>
                  <th>{t.status}</th>
                  <th>{t.vmImage}</th>
                  <th>{t.vmFlavor}</th>
                  <th>{t.vmNetwork}</th>
                  <th>{t.created}</th>
                </tr>
              </thead>
              <tbody>
                {visible.map((vm) => {
                  const flavor = flavors.get(vm.flavorId);
                  return (
                    <tr key={vm.id}>
                      <td className="col-check">
                        <input
                          type="checkbox"
                          aria-label={fill(t.vmSelect, { name: vm.name })}
                          checked={selected.has(vm.id)}
                          onChange={() => toggle(vm.id)}
                        />
                      </td>
                      <td>
                        <div className="cell-main">
                          <Link to={projectPath(projectId, `vms/${vm.id}`)}>{vm.name}</Link>
                          <span className="cell-sub mono">{vm.id}</span>
                        </div>
                      </td>
                      <td>
                        <Badge tone={statusTone(vm.status)} dot>
                          {statusText(vm.status)}
                        </Badge>
                      </td>
                      <td>{imageNames.get(vm.imageId) ?? vm.imageId}</td>
                      <td>
                        <div className="cell-main">
                          <span>{flavor?.name ?? vm.flavorId}</span>
                          {flavor && (
                            <span className="cell-sub">
                              {flavor.cpu} vCPU · {formatBytes(flavor.memoryBytes)} · {formatBytes(flavor.rootDiskBytes)}
                            </span>
                          )}
                        </div>
                      </td>
                      <td>{networkLabel(vm.subnetId)}</td>
                      <td className="muted">{formatDateTime(vm.createdAt, language)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
        {all.length > 0 && (
          <div className="table-footer">
            {fill(t.totalCount, { n: all.length })} · {fill(t.autoRefresh, { s: LIST_REFRESH_MS / 1000 })}
          </div>
        )}
      </Card>
    </>
  );
}
