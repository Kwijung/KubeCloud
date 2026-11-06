import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleAlert, Info, Play, RotateCw, Server, Square, Terminal, Trash2, TriangleAlert } from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";
import { api } from "../api";
import type { VirtualMachineDetail } from "../api";
import { projectPath, useProject } from "../app/Layout";
import { BUSY_REFRESH_MS, isVMBusy, keys, LIST_REFRESH_MS, useAllFlavors, useImages } from "../app/queries";
import { fill, formatBytes, formatDateTime, formatRelative } from "../format";
import { statusTone, useI18n } from "../i18n";
import { Alert, Badge, Button, Card, DescriptionList, Empty, PageHeader, Spinner, Tabs, useConfirm } from "../ui";
import { BootLog, SerialConsole } from "./VMConsole";

const tabValues = ["overview", "network", "disk", "console", "events"] as const;
type Tab = (typeof tabValues)[number];

export function VMDetail() {
  const { t, language, statusText, errorText } = useI18n();
  const { projectId, projectName } = useProject();
  const params = useParams();
  const vmId = params.vmId ?? "";
  const tab: Tab = (tabValues as readonly string[]).includes(params.tab ?? "") ? (params.tab as Tab) : "overview";
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const flavors = useAllFlavors();
  const images = useImages(projectId);
  const detail = useQuery({
    queryKey: keys.vm(projectId, vmId),
    queryFn: () => api.vm(projectId, vmId),
    refetchInterval: (query) => (isVMBusy(query.state.data?.runtimeStatus ?? "") ? BUSY_REFRESH_MS : LIST_REFRESH_MS),
  });

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: keys.vm(projectId, vmId) });
    void queryClient.invalidateQueries({ queryKey: keys.vms(projectId) });
  };
  const action = useMutation({
    mutationFn: (kind: "start" | "stop" | "restart" | "delete") => {
      const call = { start: api.startVM, stop: api.stopVM, restart: api.restartVM, delete: api.deleteVM }[kind];
      return call(projectId, vmId);
    },
    onSuccess: (_, kind) => {
      if (kind === "delete") {
        void queryClient.invalidateQueries({ queryKey: keys.vms(projectId) });
        navigate(projectPath(projectId, "vms"));
        return;
      }
      refresh();
    },
  });

  if (detail.isLoading) {
    return <Empty title={<Spinner label={t.loading} />} />;
  }
  if (detail.isError || !detail.data) {
    return (
      <>
        <PageHeader crumbs={[{ label: t.navVMs, to: projectPath(projectId, "vms") }]} title={vmId} />
        <Alert tone="error">{detail.error ? errorText(detail.error) : t.detailNotFound}</Alert>
      </>
    );
  }

  const data = detail.data;
  const events = data.events ?? [];
  const vm = data.virtualMachine;
  const status = data.runtimeStatus || vm.status;
  const running = status === "Running";
  const stopped = status === "Stopped";
  const flavor = flavors.data?.find((item) => item.id === vm.flavorId);
  const image = images.data?.find((item) => item.id === vm.imageId);
  const setTab = (next: Tab) => navigate(projectPath(projectId, `vms/${vmId}${next === "overview" ? "" : `/${next}`}`), { replace: true });

  async function run(kind: "start" | "stop" | "restart" | "delete") {
    if (kind !== "start") {
      const ok = await confirm({
        title: kind === "delete" ? t.vmDeleteConfirmTitle : kind === "stop" ? t.vmStopConfirmTitle : t.vmRestartConfirmTitle,
        message: fill(kind === "delete" ? t.vmDeleteConfirm : kind === "stop" ? t.vmStopConfirm : t.vmRestartConfirm, {
          names: vm.name,
          name: vm.name,
        }),
        confirmLabel: kind === "delete" ? t.vmDelete : kind === "stop" ? t.vmStop : t.vmRestart,
        cancelLabel: t.cancel,
        danger: kind !== "restart",
      });
      if (!ok) {
        return;
      }
    }
    action.mutate(kind);
  }

  const subnetLabel = data.subnet
    ? `${data.subnet.name} · ${data.subnet.cidr} · ${data.subnet.type === "public" ? t.detailSubnetPublic : t.detailSubnetPrivate}`
    : "";
  const networkLabel = data.network ? `${data.network.name} / ${data.subnet?.name ?? "?"}` : t.vmDefaultNetwork;

  return (
    <>
      <PageHeader
        crumbs={[
          { label: projectName, to: projectPath(projectId) },
          { label: t.navVMs, to: projectPath(projectId, "vms") },
          { label: vm.name },
        ]}
        title={
          <span className="row">
            <Server size={22} aria-hidden /> {vm.name}
          </span>
        }
        badge={
          <Badge tone={statusTone(status)} dot>
            {statusText(status)}
          </Badge>
        }
        subtitle={<span className="mono">{vm.id}</span>}
        actions={
          <>
            <Button variant="primary" icon={Terminal} disabled={!running} onClick={() => setTab("console")}>
              {t.vmConsole}
            </Button>
            {stopped ? (
              <Button icon={Play} loading={action.isPending && action.variables === "start"} onClick={() => run("start")}>
                {t.vmStart}
              </Button>
            ) : (
              <Button icon={Square} loading={action.isPending && action.variables === "stop"} onClick={() => run("stop")}>
                {t.vmStop}
              </Button>
            )}
            <Button icon={RotateCw} disabled={!running} loading={action.isPending && action.variables === "restart"} onClick={() => run("restart")}>
              {t.vmRestart}
            </Button>
            <Button variant="danger" icon={Trash2} loading={action.isPending && action.variables === "delete"} onClick={() => run("delete")}>
              {t.vmDelete}
            </Button>
          </>
        }
      />
      {action.isError && <Alert tone="error">{errorText(action.error)}</Alert>}
      {(data.failureReason || data.failureMessage) && (
        <Alert tone="error" title={t.detailFailure}>
          {data.failureReason && <strong className="mono">{data.failureReason}</strong>} {data.failureMessage}
        </Alert>
      )}
      <Tabs
        label={vm.name}
        value={tab}
        onChange={setTab}
        tabs={[
          { value: "overview", label: t.detailTabOverview },
          { value: "network", label: t.detailTabNetwork },
          { value: "disk", label: t.detailTabDisk },
          { value: "console", label: t.detailTabConsole },
          { value: "events", label: `${t.detailTabEvents}${events.length ? ` (${events.length})` : ""}` },
        ]}
      />

      {tab === "overview" && (
        <div className="grid-2">
          <Card title={t.detailEssentials}>
            <DescriptionList
              items={[
                { label: t.status, value: statusText(status) },
                { label: t.detailImage, value: image?.name ?? vm.imageId },
                {
                  label: t.detailFlavor,
                  value: flavor
                    ? fill(t.detailFlavorValue, { name: flavor.name, cpu: flavor.cpu, mem: formatBytes(flavor.memoryBytes) })
                    : vm.flavorId,
                },
                { label: t.created, value: `${formatDateTime(vm.createdAt, language)} (${formatRelative(vm.createdAt, language)})` },
              ]}
            />
          </Card>
          <Card title={t.detailTabNetwork}>
            <DescriptionList
              items={[
                { label: t.detailProject, value: projectName },
                { label: t.vmNetwork, value: networkLabel },
                { label: t.detailIp, value: data.vmiIp },
                { label: t.detailNode, value: data.nodeName },
              ]}
            />
          </Card>
        </div>
      )}

      {tab === "network" && <NetworkTab data={data} subnetLabel={subnetLabel} />}

      {tab === "disk" && (
        <Card title={t.detailTabDisk}>
          <DescriptionList
            items={[
              { label: t.detailDiskSize, value: data.pvcCapacity || (flavor ? formatBytes(flavor.rootDiskBytes) : "") },
              { label: t.detailImage, value: image ? `${image.name} (${image.sourceUrl})` : vm.imageId },
              { label: t.detailDiskImport, value: [data.dataVolumePhase, data.dataVolumeStatus].filter(Boolean).join(" · ") },
              { label: t.detailDiskVolume, value: [`${vm.id}-rootdisk`, data.pvcPhase].filter(Boolean).join(" · ") },
            ]}
          />
        </Card>
      )}

      {tab === "console" && (
        <div className="stack">
          <SerialConsole
            projectId={projectId}
            vmId={vmId}
            running={running}
            onShowBootLog={() => document.getElementById("boot-log")?.scrollIntoView({ behavior: "smooth" })}
          />
          <div id="boot-log">
            <BootLog projectId={projectId} vmId={vmId} running={running} />
          </div>
        </div>
      )}

      {tab === "events" && (
        <Card title={t.detailTabEvents} flush>
          {events.length === 0 ? (
            <Empty title={t.detailNoEvents} />
          ) : (
            <ul className="events">
              {events.map((event, index) => {
                const Icon = event.type === "Warning" ? TriangleAlert : event.type === "Error" ? CircleAlert : Info;
                const color = event.type === "Normal" ? "var(--c-text-3)" : "var(--c-warning)";
                return (
                  <li key={`${event.reason}-${index}`}>
                    <Icon size={18} color={color} aria-hidden />
                    <div className="cell-main">
                      <strong>{event.reason}</strong>
                      <span className="event-message">{event.message}</span>
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </Card>
      )}
    </>
  );
}

function NetworkTab({ data, subnetLabel }: { data: VirtualMachineDetail; subnetLabel: string }) {
  const { t } = useI18n();
  const subnet = data.subnet;
  return (
    <Card title={t.detailTabNetwork}>
      <DescriptionList
        items={
          data.network
            ? [
                { label: t.detailVpc, value: `${data.network.name}${data.network.cidr ? ` (${data.network.cidr})` : ""}` },
                { label: t.detailSubnet, value: subnetLabel },
                { label: t.detailGateway, value: subnet?.gateway ?? "" },
                { label: t.detailIp, value: data.vmiIp },
                {
                  label: t.detailEgress,
                  value: subnet?.type === "public" ? t.detailEgressPublic : t.detailEgressPrivate,
                },
              ]
            : [
                { label: t.detailVpc, value: t.vmDefaultNetwork },
                { label: t.detailIp, value: data.vmiIp },
                { label: t.detailEgress, value: t.detailEgressPod },
              ]
        }
      />
    </Card>
  );
}
