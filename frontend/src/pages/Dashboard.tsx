import { AlertCircle, Image as ImageIcon, Loader, Network, Plus } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { projectPath, useProject } from "../app/Layout";
import { flavorMap, isVMBusy, useAllFlavors, useImages, useNetworks, useAllSubnets, useVMs } from "../app/queries";
import { fill, formatBytes, formatRelative } from "../format";
import { statusTone, useI18n } from "../i18n";
import { Badge, Button, Card, Empty, PageHeader } from "../ui";

export function Dashboard() {
  const { t, language, statusText } = useI18n();
  const { projectId, projectName } = useProject();
  const navigate = useNavigate();
  const vms = useVMs(projectId);
  const images = useImages(projectId);
  const networks = useNetworks(projectId);
  const subnets = useAllSubnets(projectId, networks.data);
  const flavors = flavorMap(useAllFlavors().data);

  const list = vms.data ?? [];
  const running = list.filter((vm) => vm.status === "Running");
  const stopped = list.filter((vm) => vm.status === "Stopped");
  const attention = list.filter((vm) => vm.status === "Failed" || vm.status === "Missing" || isVMBusy(vm.status));
  const runningFlavors = running.map((vm) => flavors.get(vm.flavorId)).filter((flavor) => flavor !== undefined);
  const cpu = runningFlavors.reduce((sum, flavor) => sum + flavor.cpu, 0);
  const memory = runningFlavors.reduce((sum, flavor) => sum + flavor.memoryBytes, 0);
  const disk = list.reduce((sum, vm) => sum + (flavors.get(vm.flavorId)?.rootDiskBytes ?? 0), 0);
  const subnetCount = [...subnets.byNetwork.values()].reduce((sum, items) => sum + items.length, 0);
  const projectImages = (images.data ?? []).filter((image) => image.scope === "project");
  const imageNames = new Map((images.data ?? []).map((image) => [image.id, image.name]));
  const names = (items: typeof list) => items.map((vm) => vm.name).join(" · ");

  const stats = [
    { label: t.dashTotalVMs, count: list.length, note: projectName, color: "var(--c-text-3)" },
    { label: t.dashRunning, count: running.length, note: names(running), color: "var(--c-success)" },
    { label: t.dashStopped, count: stopped.length, note: names(stopped), color: "var(--c-border-strong)" },
    { label: t.dashAttention, count: attention.length, note: names(attention), color: "var(--c-error)" },
  ];

  const recent = [
    ...list.map((vm) => ({
      key: `vm-${vm.id}`,
      name: vm.name,
      kind: t.kindVM,
      status: vm.status,
      createdAt: vm.createdAt,
      to: projectPath(projectId, `vms/${vm.id}`),
    })),
    ...(networks.data ?? []).map((network) => ({
      key: `vpc-${network.id}`,
      name: network.name,
      kind: t.kindVPC,
      status: network.status,
      createdAt: network.createdAt,
      to: projectPath(projectId, `vpcs/${network.id}`),
    })),
    ...projectImages.map((image) => ({
      key: `img-${image.id}`,
      name: image.name,
      kind: t.kindImage,
      status: image.status,
      createdAt: image.createdAt,
      to: projectPath(projectId, "images"),
    })),
  ]
    .sort((a, b) => b.createdAt.localeCompare(a.createdAt))
    .slice(0, 6);

  return (
    <>
      <PageHeader
        title={t.navDashboard}
        subtitle={fill(t.dashSubtitle, { project: projectName })}
        actions={
          <>
            <Button icon={ImageIcon} onClick={() => navigate(projectPath(projectId, "images"))}>
              {t.imgRegister}
            </Button>
            <Button icon={Network} onClick={() => navigate(projectPath(projectId, "vpcs"))}>
              {t.vpcCreate}
            </Button>
            <Button variant="primary" icon={Plus} onClick={() => navigate(projectPath(projectId, "vms/new"))}>
              {t.vmCreate}
            </Button>
          </>
        }
      />

      <section className="stat-grid" aria-label={t.navVMs}>
        {stats.map((stat) => (
          <Link key={stat.label} to={projectPath(projectId, "vms")} className="stat">
            <span className="stat-label">
              <span className="stat-dot" style={{ background: stat.color }} aria-hidden />
              {stat.label}
            </span>
            <span className="stat-value">{vms.isLoading ? "–" : stat.count}</span>
            <span className="stat-note" title={stat.note}>
              {stat.note || " "}
            </span>
          </Link>
        ))}
      </section>

      <div className="grid-2">
        <Card
          title={t.dashAttentionTitle}
          actions={attention.length > 0 && <Badge tone="error">{attention.length}</Badge>}
          flush
        >
          {attention.length === 0 ? (
            <Empty title={t.dashAttentionEmpty} />
          ) : (
            <ul className="attention-list">
              {attention.map((vm) => {
                const failed = vm.status === "Failed" || vm.status === "Missing";
                return (
                  <li key={vm.id}>
                    {failed ? (
                      <AlertCircle size={20} color="var(--c-error)" aria-hidden />
                    ) : (
                      <Loader size={20} color="var(--c-info)" aria-hidden />
                    )}
                    <div className="cell-main" style={{ flex: 1 }}>
                      <span className="row">
                        <Link to={projectPath(projectId, `vms/${vm.id}`)}>{vm.name}</Link>
                        <Badge tone={statusTone(vm.status)} dot>
                          {statusText(vm.status)}
                        </Badge>
                      </span>
                      <span className="cell-sub">
                        {imageNames.get(vm.imageId) ?? vm.imageId} · {flavors.get(vm.flavorId)?.name ?? vm.flavorId} · {formatRelative(vm.createdAt, language)}
                      </span>
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </Card>

        <Card title={t.dashUsage} actions={<Badge>{t.dashQuotaUnset}</Badge>}>
          <div className="usage-grid">
            <Usage label={t.vcpu} value={String(cpu)} note={t.dashUsageRunning} />
            <Usage label={t.memory} value={formatBytes(memory) || "0 GiB"} note={t.dashUsageRunning} />
            <Usage label={t.disk} value={formatBytes(disk) || "0 GiB"} note={fill(t.dashUsageDisks, { n: list.length })} />
            <Usage label={t.kindVPC} value={String(networks.data?.length ?? 0)} note={fill(t.dashUsageSubnets, { n: subnetCount })} />
            <Usage label={t.navImages} value={String(projectImages.length)} note={t.dashUsageImages} />
          </div>
        </Card>
      </div>

      <Card title={t.dashRecent} actions={<Link to={projectPath(projectId, "vms")}>{t.viewAll}</Link>} flush>
        {recent.length === 0 ? (
          <Empty title={t.vmEmpty} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t.name}</th>
                  <th>{t.kind}</th>
                  <th>{t.status}</th>
                  <th>{t.created}</th>
                </tr>
              </thead>
              <tbody>
                {recent.map((item) => (
                  <tr key={item.key}>
                    <td>
                      <Link to={item.to}>{item.name}</Link>
                    </td>
                    <td className="muted">{item.kind}</td>
                    <td>
                      <Badge tone={statusTone(item.status)} dot>
                        {statusText(item.status)}
                      </Badge>
                    </td>
                    <td className="muted">{formatRelative(item.createdAt, language)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </>
  );
}

function Usage({ label, value, note }: { label: string; value: string; note: string }) {
  return (
    <div className="cell-main">
      <span className="cell-sub">{label}</span>
      <span className="usage-value">{value}</span>
      <span className="cell-sub">{note}</span>
    </div>
  );
}
