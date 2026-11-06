import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Rocket } from "lucide-react";
import type { FormEvent, ReactNode } from "react";
import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api } from "../api";
import type { Flavor, Image } from "../api";
import { projectPath, useProject } from "../app/Layout";
import { keys, useFlavors, useImages, useNetworks, useAllSubnets } from "../app/queries";
import { fill, formatBytes } from "../format";
import { statusTone, useI18n } from "../i18n";
import { Alert, Badge, Button, Card, DescriptionList, Field, Input, PageHeader, Select, Textarea } from "../ui";

export function VMCreate() {
  const { t, statusText, errorText } = useI18n();
  const { projectId, projectName } = useProject();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const images = useImages(projectId);
  const flavors = useFlavors();
  const networks = useNetworks(projectId);
  const subnets = useAllSubnets(projectId, networks.data);

  const [name, setName] = useState("");
  const [imageId, setImageId] = useState("");
  const [flavorId, setFlavorId] = useState("");
  const [networkId, setNetworkId] = useState("");
  const [subnetId, setSubnetId] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [sshKey, setSshKey] = useState("");

  const allImages = images.data ?? [];
  const image = allImages.find((item) => item.id === imageId && item.status === "Ready");
  const minDisk = image?.minRootDiskBytes ?? 0;
  const fits = (flavor: Flavor) => flavor.rootDiskBytes >= minDisk;
  const flavorList = flavors.data ?? [];
  const flavor = flavorList.find((item) => item.id === flavorId);
  const network = networks.data?.find((item) => item.id === networkId);
  const vpcSubnets = networkId ? subnets.byNetwork.get(networkId) ?? [] : [];
  const subnet = vpcSubnets.find((item) => item.id === subnetId);

  // Pick the first ready image, then keep the flavor one whose disk holds the image.
  useEffect(() => {
    if (!image) {
      const firstReady = allImages.find((item) => item.status === "Ready");
      if (firstReady && firstReady.id !== imageId) {
        setImageId(firstReady.id);
      }
    }
  }, [allImages, image, imageId]);
  useEffect(() => {
    if (flavorList.length > 0 && (!flavor || !fits(flavor))) {
      const next = flavorList.find(fits);
      if (next && next.id !== flavorId) {
        setFlavorId(next.id);
      }
    }
    // fits only depends on minDisk.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [flavorList, flavor, flavorId, minDisk]);

  const create = useMutation({
    mutationFn: () =>
      api.createVM(projectId, {
        name: name.trim(),
        imageId,
        flavorId,
        subnetId: subnetId || undefined,
        cloudInit: {
          username: username.trim() || undefined,
          password: password || undefined,
          sshPublicKey: sshKey.trim() || undefined,
        },
      }),
    onSuccess: (vm) => {
      void queryClient.invalidateQueries({ queryKey: keys.vms(projectId) });
      navigate(projectPath(projectId, `vms/${vm.id}`));
    },
  });

  const subnetMissing = networkId !== "" && !subnet;
  const canCreate = name.trim() !== "" && Boolean(image) && Boolean(flavor && fits(flavor)) && !subnetMissing;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (canCreate && !create.isPending) {
      create.mutate();
    }
  }

  const groups: Array<{ label: string; items: Image[] }> = [
    { label: t.createPublicImages, items: allImages.filter((item) => item.scope === "public") },
    { label: `${t.createProjectImages} · ${projectName}`, items: allImages.filter((item) => item.scope === "project") },
  ];

  return (
    <form onSubmit={submit} className="stack">
      <PageHeader
        crumbs={[
          { label: projectName, to: projectPath(projectId) },
          { label: t.navVMs, to: projectPath(projectId, "vms") },
          { label: t.createTitle },
        ]}
        title={t.createTitle}
        subtitle={t.createSubtitle}
      />
      <div className="create-layout">
        <div className="stack">
          <Card title={<Step n={1}>{t.createStepName}</Step>}>
            <Field label={t.createNameLabel} hint={t.createNameHint}>
              {(id) => (
                <Input id={id} value={name} onChange={(event) => setName(event.target.value)} placeholder="web-01" required autoFocus />
              )}
            </Field>
          </Card>

          <Card
            title={<Step n={2}>{t.createStepImage}</Step>}
            actions={<Link to={projectPath(projectId, "images")}>{t.createRegisterImage}</Link>}
          >
            {allImages.length === 0 && !images.isLoading && <Alert tone="warning">{t.createNoImages}</Alert>}
            {groups.map(
              (group) =>
                group.items.length > 0 && (
                  <div key={group.label} role="radiogroup" aria-label={group.label}>
                    <div className="choice-group-label">{group.label}</div>
                    <div className="choice-grid">
                      {group.items.map((item) => (
                        <label className="choice" key={item.id}>
                          <input
                            type="radio"
                            name="image"
                            value={item.id}
                            checked={item.id === imageId}
                            disabled={item.status !== "Ready"}
                            onChange={() => setImageId(item.id)}
                          />
                          <span className="row spread">
                            <span className="choice-title">{item.name}</span>
                            {item.status !== "Ready" && (
                              <Badge tone={statusTone(item.status)} dot>
                                {statusText(item.status)}
                              </Badge>
                            )}
                          </span>
                          <span className="choice-sub">
                            {item.osType} ·{" "}
                            {item.minRootDiskBytes
                              ? fill(t.createMinFlavor, { size: formatBytes(item.minRootDiskBytes) })
                              : t.createSizeUnknown}
                          </span>
                        </label>
                      ))}
                    </div>
                  </div>
                ),
            )}
            {allImages.some((item) => item.status === "Importing") && <p className="muted small">{t.createImageImporting}</p>}
          </Card>

          <Card title={<Step n={3}>{t.createStepFlavor}</Step>} flush>
            <div className="table-wrap" role="radiogroup" aria-label={t.createStepFlavor}>
              <table className="table flavor-table">
                <thead>
                  <tr>
                    <th>{t.vmFlavor}</th>
                    <th>{t.vcpu}</th>
                    <th>{t.memory}</th>
                    <th>{t.createRootDisk}</th>
                    <th>{t.vmImage}</th>
                  </tr>
                </thead>
                <tbody>
                  {flavorList.map((item) => {
                    const ok = fits(item);
                    return (
                      <tr key={item.id} className={item.id === flavorId ? "is-selected" : ok ? "" : "is-disabled"}>
                        <td>
                          <label>
                            <input
                              type="radio"
                              name="flavor"
                              value={item.id}
                              checked={item.id === flavorId}
                              disabled={!ok}
                              onChange={() => setFlavorId(item.id)}
                            />
                            <strong>{item.name}</strong>
                          </label>
                        </td>
                        <td>{item.cpu}</td>
                        <td>{formatBytes(item.memoryBytes)}</td>
                        <td>{formatBytes(item.rootDiskBytes)}</td>
                        <td>
                          <Badge tone={ok ? "success" : "neutral"}>{ok ? t.createFits : t.createTooSmall}</Badge>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </Card>

          <Card title={<Step n={4}>{t.createStepNetwork}</Step>}>
            <div className="stack">
              <Field label={t.detailVpc}>
                {(id) => (
                  <Select
                    id={id}
                    value={networkId}
                    onChange={(event) => {
                      setNetworkId(event.target.value);
                      setSubnetId("");
                    }}
                  >
                    <option value="">{t.createNoVpc}</option>
                    {(networks.data ?? []).map((item) => (
                      <option key={item.id} value={item.id}>
                        {item.name}
                        {item.cidr ? ` (${item.cidr})` : ""}
                      </option>
                    ))}
                  </Select>
                )}
              </Field>
              {networkId !== "" && (
                <div role="radiogroup" aria-label={t.createSubnetLabel}>
                  <div className="choice-group-label">{t.createSubnetLabel}</div>
                  {vpcSubnets.length === 0 ? (
                    <Alert tone="warning">{t.createNoSubnets}</Alert>
                  ) : (
                    <div className="choice-grid">
                      {vpcSubnets.map((item) => (
                        <label className="choice" key={item.id}>
                          <input
                            type="radio"
                            name="subnet"
                            value={item.id}
                            checked={item.id === subnetId}
                            onChange={() => setSubnetId(item.id)}
                          />
                          <span className="row spread">
                            <span className="choice-title">{item.name}</span>
                            <Badge tone={item.type === "public" ? "info" : "neutral"}>
                              {item.type === "public" ? t.subnetPublic : t.subnetPrivate}
                            </Badge>
                          </span>
                          <span className="choice-sub">
                            {item.cidr} · {item.type === "public" ? t.subnetPublicHint : t.subnetPrivateHint}
                          </span>
                        </label>
                      ))}
                    </div>
                  )}
                </div>
              )}
              <Alert tone="info">{networkId ? t.createDnsNote : t.createPodNetworkNote}</Alert>
            </div>
          </Card>

          <Card title={<Step n={5}>{t.createStepAccess}</Step>}>
            <div className="form-grid">
              <Field label={t.createUsername} hint={t.createUsernameHint}>
                {(id) => <Input id={id} value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="off" />}
              </Field>
              <Field label={t.createPassword}>
                {(id) => (
                  <Input
                    id={id}
                    type="password"
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                    autoComplete="new-password"
                  />
                )}
              </Field>
            </div>
            <div style={{ marginTop: 14 }}>
              <Field label={t.createSshKey}>
                {(id) => (
                  <Textarea
                    id={id}
                    value={sshKey}
                    onChange={(event) => setSshKey(event.target.value)}
                    placeholder="ssh-ed25519 AAAA..."
                    rows={2}
                  />
                )}
              </Field>
            </div>
          </Card>
        </div>

        <div className="create-summary">
          <Card title={t.createSummary}>
            <div className="stack">
              <DescriptionList
                items={[
                  { label: t.name, value: name.trim() || t.createNotChosen },
                  { label: t.vmImage, value: image?.name ?? t.createNotChosen },
                  {
                    label: t.vmFlavor,
                    value: flavor ? `${flavor.name} · ${flavor.cpu} vCPU · ${formatBytes(flavor.memoryBytes)}` : t.createNotChosen,
                  },
                  { label: t.createRootDisk, value: flavor ? formatBytes(flavor.rootDiskBytes) : "" },
                  {
                    label: t.vmNetwork,
                    value: network ? `${network.name} / ${subnet?.name ?? "?"}` : t.vmDefaultNetwork,
                  },
                ]}
              />
              {flavor && (
                <div className="cell-main">
                  <span className="row">
                    <span className="small">{t.createUsage}</span>
                    <Badge>{t.dashQuotaUnset}</Badge>
                  </span>
                  <span className="cell-sub">
                    {fill(t.createUsageValue, {
                      cpu: flavor.cpu,
                      mem: formatBytes(flavor.memoryBytes),
                      disk: formatBytes(flavor.rootDiskBytes),
                    })}
                  </span>
                </div>
              )}
              {subnetMissing && vpcSubnets.length > 0 && <Alert tone="warning">{t.createSubnetRequired}</Alert>}
              {create.isError && <Alert tone="error">{errorText(create.error)}</Alert>}
              <Button type="submit" variant="primary" icon={Rocket} disabled={!canCreate} loading={create.isPending}>
                {create.isPending ? t.creating : t.createSubmit}
              </Button>
              <Button variant="ghost" onClick={() => navigate(projectPath(projectId, "vms"))}>
                {t.cancel}
              </Button>
            </div>
          </Card>
        </div>
      </div>
    </form>
  );
}

function Step({ n, children }: { n: number; children: ReactNode }) {
  return (
    <span className="step-title">
      <span className="step-number">{n}</span>
      {children}
    </span>
  );
}
