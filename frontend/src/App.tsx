import { Activity, Boxes, CircleStop, Cpu, HardDrive, Image, Network, Play, Server, Terminal, Trash2 } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { ClipboardEvent, FormEvent, KeyboardEvent } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  createNetwork,
  createImage,
  createSubnet,
  createVirtualMachine,
  deleteNetwork,
  deleteImage,
  deleteSubnet,
  deleteVirtualMachine,
  fetchFlavors,
  fetchHealth,
  fetchImages,
  fetchNetworks,
  fetchPreflight,
  fetchProjects,
  fetchSubnets,
  fetchVirtualMachineDetail,
  fetchVirtualMachines,
  startVirtualMachine,
  stopVirtualMachine,
  virtualMachineConsoleStreamURL,
  type Flavor,
} from "./api";
import { getInitialLanguage, languageOptions, messages, saveLanguage, translateApiError, translateStatus } from "./i18n";
import type { Language } from "./i18n";

const navItems = [
  { labelKey: "navProjects", icon: Boxes },
  { labelKey: "navVirtualMachines", icon: Server },
  { labelKey: "navImages", icon: Image },
  { labelKey: "navStorage", icon: HardDrive },
] as const;

// Public cloud images that boot with cloud-init on KubeVirt. The root disk must be larger than the image's
// virtual size (Ubuntu 24.04 3.5GiB, Ubuntu 22.04 2.2GiB, Debian 12 3GiB, Rocky 9 10GiB), hence minFlavor.
const imagePresets = [
  { name: "Ubuntu 24.04", minFlavor: "medium", url: "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img" },
  { name: "Ubuntu 22.04", minFlavor: "medium", url: "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img" },
  { name: "Debian 12", minFlavor: "medium", url: "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2" },
  { name: "Rocky Linux 9", minFlavor: "large", url: "https://dl.rockylinux.org/pub/rocky/9/images/x86_64/Rocky-9-GenericCloud-Base.latest.x86_64.qcow2" },
] as const;

export function App() {
  const [language, setLanguage] = useState<Language>(() => getInitialLanguage());
  const [selectedProjectId, setSelectedProjectId] = useState<string>("");
  const [vmName, setVmName] = useState("");
  const [selectedImageId, setSelectedImageId] = useState("");
  const [selectedFlavorId, setSelectedFlavorId] = useState("");
  const [vmNetworkId, setVmNetworkId] = useState("");
  const [newImageName, setNewImageName] = useState("");
  const [newImageUrl, setNewImageUrl] = useState("");
  const [newImageOsType, setNewImageOsType] = useState<"linux" | "windows">("linux");
  const [selectedSubnetId, setSelectedSubnetId] = useState("");
  const [cloudInitUsername, setCloudInitUsername] = useState("cirros");
  const [cloudInitPassword, setCloudInitPassword] = useState("gocubsgo");
  const [cloudInitSSHPublicKey, setCloudInitSSHPublicKey] = useState("");
  const [selectedVMId, setSelectedVMId] = useState("");
  const [consoleVMId, setConsoleVMId] = useState("");
  const [consoleOutput, setConsoleOutput] = useState("");
  const [consoleState, setConsoleState] = useState<"idle" | "connecting" | "connected" | "disconnected" | "error">("idle");
  const consoleSocket = useRef<WebSocket | null>(null);
  const consoleTerminalRef = useRef<HTMLDivElement | null>(null);
  const [selectedNetworkId, setSelectedNetworkId] = useState("");
  const [newNetworkName, setNewNetworkName] = useState("");
  const [newNetworkCidr, setNewNetworkCidr] = useState("");
  const [newSubnetName, setNewSubnetName] = useState("");
  const [newSubnetCidr, setNewSubnetCidr] = useState("");
  const [newSubnetType, setNewSubnetType] = useState<"private" | "public">("private");
  const queryClient = useQueryClient();
  const t = messages[language];
  const health = useQuery({
    queryKey: ["health"],
    queryFn: fetchHealth,
    retry: 1,
  });
  const projects = useQuery({
    queryKey: ["projects"],
    queryFn: fetchProjects,
  });
  const flavors = useQuery({
    queryKey: ["flavors"],
    queryFn: fetchFlavors,
  });
  const preflight = useQuery({
    queryKey: ["preflight"],
    queryFn: fetchPreflight,
    refetchInterval: 15000,
  });
  const selectedProject = useMemo(
    () => projects.data?.find((project) => project.id === selectedProjectId),
    [projects.data, selectedProjectId],
  );
  const images = useQuery({
    queryKey: ["images", selectedProjectId],
    queryFn: () => fetchImages(selectedProjectId),
    enabled: selectedProjectId !== "",
  });
  const virtualMachines = useQuery({
    queryKey: ["vms", selectedProjectId],
    queryFn: () => fetchVirtualMachines(selectedProjectId),
    enabled: selectedProjectId !== "",
  });
  const selectedVMDetail = useQuery({
    queryKey: ["vm-detail", selectedProjectId, selectedVMId],
    queryFn: () => fetchVirtualMachineDetail(selectedProjectId, selectedVMId),
    enabled: selectedProjectId !== "" && selectedVMId !== "",
  });
  const createVM = useMutation({
    mutationFn: () =>
      createVirtualMachine(selectedProjectId, {
        name: vmName.trim(),
        imageId: selectedImageId,
        flavorId: selectedFlavorId,
        subnetId: selectedSubnetId || undefined,
        cloudInit: {
          username: cloudInitUsername.trim(),
          password: cloudInitPassword.trim(),
          sshPublicKey: cloudInitSSHPublicKey.trim() || undefined,
        },
      }),
    onSuccess: () => {
      setVmName("");
      void queryClient.invalidateQueries({ queryKey: ["vms", selectedProjectId] });
      void queryClient.invalidateQueries({ queryKey: ["vm-detail", selectedProjectId] });
    },
  });
  const createImageMutation = useMutation({
    mutationFn: () =>
      createImage(selectedProjectId, { name: newImageName.trim(), osType: newImageOsType, sourceUrl: newImageUrl.trim() }),
    onSuccess: () => {
      setNewImageName("");
      setNewImageUrl("");
      setNewImageOsType("linux");
      void queryClient.invalidateQueries({ queryKey: ["images", selectedProjectId] });
    },
  });
  const deleteImageMutation = useMutation({
    mutationFn: (imageId: string) => deleteImage(selectedProjectId, imageId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["images", selectedProjectId] });
    },
  });
  const deleteVM = useMutation({
    mutationFn: (vmId: string) => deleteVirtualMachine(selectedProjectId, vmId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["vms", selectedProjectId] });
      void queryClient.invalidateQueries({ queryKey: ["vm-detail", selectedProjectId] });
    },
  });
  const startVM = useMutation({
    mutationFn: (vmId: string) => startVirtualMachine(selectedProjectId, vmId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["vms", selectedProjectId] });
      void queryClient.invalidateQueries({ queryKey: ["vm-detail", selectedProjectId] });
    },
  });
  const stopVM = useMutation({
    mutationFn: (vmId: string) => stopVirtualMachine(selectedProjectId, vmId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["vms", selectedProjectId] });
      void queryClient.invalidateQueries({ queryKey: ["vm-detail", selectedProjectId] });
    },
  });
  const networks = useQuery({
    queryKey: ["networks", selectedProjectId],
    queryFn: () => fetchNetworks(selectedProjectId),
    enabled: selectedProjectId !== "",
  });
  const vmSubnets = useQuery({
    queryKey: ["subnets", selectedProjectId, vmNetworkId],
    queryFn: () => fetchSubnets(selectedProjectId, vmNetworkId),
    enabled: selectedProjectId !== "" && vmNetworkId !== "",
  });
  const subnets = useQuery({
    queryKey: ["subnets", selectedProjectId, selectedNetworkId],
    queryFn: () => fetchSubnets(selectedProjectId, selectedNetworkId),
    enabled: selectedProjectId !== "" && selectedNetworkId !== "",
  });
  const createNetworkMutation = useMutation({
    mutationFn: () => createNetwork(selectedProjectId, newNetworkName.trim(), newNetworkCidr.trim()),
    onSuccess: () => {
      setNewNetworkName("");
      setNewNetworkCidr("");
      void queryClient.invalidateQueries({ queryKey: ["networks", selectedProjectId] });
    },
  });
  const deleteNetworkMutation = useMutation({
    mutationFn: (networkId: string) => deleteNetwork(selectedProjectId, networkId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["networks", selectedProjectId] });
    },
  });
  const createSubnetMutation = useMutation({
    mutationFn: () =>
      createSubnet(selectedProjectId, selectedNetworkId, {
        name: newSubnetName.trim(),
        cidr: newSubnetCidr.trim(),
        type: newSubnetType,
      }),
    onSuccess: () => {
      setNewSubnetName("");
      setNewSubnetCidr("");
      setNewSubnetType("private");
      void queryClient.invalidateQueries({ queryKey: ["subnets", selectedProjectId, selectedNetworkId] });
    },
  });
  const deleteSubnetMutation = useMutation({
    mutationFn: (subnetId: string) => deleteSubnet(selectedProjectId, selectedNetworkId, subnetId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["subnets", selectedProjectId, selectedNetworkId] });
    },
  });
  useEffect(() => {
    saveLanguage(language);
  }, [language]);

  useEffect(() => {
    return () => {
      consoleSocket.current?.close();
    };
  }, []);

  useEffect(() => {
    if (consoleVMId !== "") {
      consoleTerminalRef.current?.focus();
    }
  }, [consoleVMId]);

  useEffect(() => {
    const terminal = consoleTerminalRef.current;
    if (terminal) {
      terminal.scrollTop = terminal.scrollHeight;
    }
  }, [consoleOutput]);

  useEffect(() => {
    if (!selectedProjectId && projects.data && projects.data.length > 0) {
      setSelectedProjectId(projects.data[0].id);
    }
  }, [projects.data, selectedProjectId]);

  const readyImages = useMemo(() => (images.data ?? []).filter((image) => image.status === "Ready"), [images.data]);

  useEffect(() => {
    if (!readyImages.some((image) => image.id === selectedImageId)) {
      setSelectedImageId(readyImages[0]?.id ?? "");
    }
  }, [readyImages, selectedImageId]);

  const selectedImageMinDisk = readyImages.find((image) => image.id === selectedImageId)?.minRootDiskBytes ?? 0;
  const flavorFitsImage = (flavor: Flavor) => flavor.rootDiskBytes >= selectedImageMinDisk;

  // Keep the selected flavor valid for the selected image: move to the first flavor whose disk holds it.
  useEffect(() => {
    const available = flavors.data ?? [];
    const current = available.find((flavor) => flavor.id === selectedFlavorId);
    if (available.length > 0 && (!current || !flavorFitsImage(current))) {
      setSelectedFlavorId((available.find(flavorFitsImage) ?? available[0]).id);
    }
    // flavorFitsImage only depends on selectedImageMinDisk.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [flavors.data, selectedFlavorId, selectedImageMinDisk]);

  useEffect(() => {
    if (virtualMachines.data && virtualMachines.data.length > 0 && !virtualMachines.data.some((vm) => vm.id === selectedVMId)) {
      setSelectedVMId(virtualMachines.data[0].id);
    }
  }, [selectedVMId, virtualMachines.data]);

  useEffect(() => {
    setSelectedNetworkId("");
    setVmNetworkId("");
    setSelectedSubnetId("");
  }, [selectedProjectId]);

  const isCatalogLoading = projects.isLoading || flavors.isLoading || images.isLoading || virtualMachines.isLoading;
  const projectCount = projects.data?.length ?? 0;
  const vmCount = virtualMachines.data?.length ?? 0;
  const imageCount = images.data?.length ?? 0;
  const vmSubnetMissing = vmNetworkId !== "" && selectedSubnetId === "";
  const canCreateVM =
    selectedProjectId !== "" && vmName.trim() !== "" && selectedImageId !== "" && selectedFlavorId !== "" && !vmSubnetMissing;
  const canCreateImage = selectedProjectId !== "" && newImageName.trim() !== "" && newImageUrl.trim() !== "";
  const canCreateNetwork = selectedProjectId !== "" && newNetworkName.trim() !== "" && newNetworkCidr.trim() !== "";
  const selectedNetworkCidr = (networks.data ?? []).find((network) => network.id === selectedNetworkId)?.cidr ?? "";
  const subnetCidrPlaceholder = selectedNetworkCidr
    ? t.subnetCidrWithin.replace("{cidr}", selectedNetworkCidr)
    : t.subnetCidrLabel;
  const canCreateSubnet = selectedProjectId !== "" && selectedNetworkId !== "" && newSubnetName.trim() !== "" && newSubnetCidr.trim() !== "";
  const healthStatus = health.data?.status ?? (health.isLoading ? "checking" : "offline");

  function handleCreateVM(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (canCreateVM) {
      createVM.mutate();
    }
  }

  function handleCreateImage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (canCreateImage) {
      createImageMutation.mutate();
    }
  }

  function applyImagePreset(presetName: string) {
    const preset = imagePresets.find((item) => item.name === presetName);
    if (preset) {
      setNewImageName(preset.name);
      setNewImageUrl(preset.url);
      setNewImageOsType("linux");
    }
  }

  function handleCreateNetwork(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (canCreateNetwork) {
      createNetworkMutation.mutate();
    }
  }

  function handleCreateSubnet(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (canCreateSubnet) {
      createSubnetMutation.mutate();
    }
  }

  function openConsole(vmId: string) {
    consoleSocket.current?.close();
    setConsoleVMId(vmId);
    setConsoleOutput("");
    setConsoleState("connecting");
    const socket = new WebSocket(virtualMachineConsoleStreamURL(selectedProjectId, vmId));
    socket.binaryType = "arraybuffer";
    consoleSocket.current = socket;
    socket.onopen = () => {
      setConsoleState("connected");
      setConsoleOutput((current) => `${current}${t.consoleConnected}\n`);
    };
    socket.onmessage = async (event) => {
      const text = await decodeConsoleMessage(event.data);
      setConsoleOutput((current) => `${current}${text}`);
    };
    socket.onerror = () => {
      setConsoleState("error");
      setConsoleOutput((current) => `${current}\n${t.consoleUnavailable}\n`);
    };
    socket.onclose = () => {
      setConsoleState((current) => (current === "error" ? "error" : "disconnected"));
      setConsoleOutput((current) => (current.endsWith(`\n${t.consoleDisconnected}\n`) ? current : `${current}\n${t.consoleDisconnected}\n`));
    };
  }

  function closeConsole() {
    consoleSocket.current?.close();
    consoleSocket.current = null;
    setConsoleVMId("");
    setConsoleState("idle");
  }

  function handleConsoleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    const socket = consoleSocket.current;
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      return;
    }
    const value = consoleKeyValue(event);
    if (value === "") {
      return;
    }
    event.preventDefault();
    socket.send(value);
  }

  function handleConsolePaste(event: ClipboardEvent<HTMLDivElement>) {
    const socket = consoleSocket.current;
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      return;
    }
    const text = event.clipboardData.getData("text");
    if (text === "") {
      return;
    }
    event.preventDefault();
    socket.send(text.replace(/\n/g, "\r"));
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">K</div>
          <div>
            <strong>Kloudia</strong>
            <span>{t.privateCloud}</span>
          </div>
        </div>
        <nav>
          {navItems.map((item) => {
            const Icon = item.icon;
            return (
              <button className="nav-item" key={item.labelKey}>
                <Icon size={18} />
                <span>{t[item.labelKey]}</span>
              </button>
            );
          })}
        </nav>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <div>
            <h1>{t.pageTitle}</h1>
            <p>{t.pageSubtitle}</p>
          </div>
          <div className="topbar-actions">
            <label className="language-select">
              <span>{t.language}</span>
              <select value={language} onChange={(event) => setLanguage(event.target.value as Language)}>
                {languageOptions.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
            <div className="status-pill">
              <Activity size={16} />
              <span>{translateStatus(healthStatus, language)}</span>
            </div>
          </div>
        </header>

        <section className="summary-grid">
          <div className="metric">
            <span>{t.metricProjects}</span>
            <strong>{projectCount}</strong>
          </div>
          <div className="metric">
            <span>{t.metricVMs}</span>
            <strong>{vmCount}</strong>
          </div>
          <div className="metric">
            <span>{t.metricImages}</span>
            <strong>{imageCount}</strong>
          </div>
          <div className="metric">
            <span>{t.metricAPI}</span>
            <strong>{health.data?.version ?? "dev"}</strong>
          </div>
        </section>

        <section className="panel preflight-panel">
          <div className="panel-header">
            <div>
              <h2>{t.preflight}</h2>
              <p>{t.preflightDescription}</p>
            </div>
            <span className={`state state-${(preflight.data?.status ?? "unknown").toLowerCase()}`}>
              {preflight.data?.status ?? "-"}
            </span>
          </div>
          <div className="preflight-grid">
            {(preflight.data?.checks ?? []).map((check) => (
              <div className="preflight-check" key={check.name}>
                <span className={`state state-${check.status.toLowerCase()}`}>{check.status}</span>
                <strong>{check.name}</strong>
                <p>{check.message}</p>
              </div>
            ))}
            {preflight.isLoading && <div className="muted-row">{t.statusChecking}</div>}
          </div>
        </section>

        <section className="split-layout">
          <div className="panel">
            <div className="panel-header">
              <div>
                <h2>{t.projectsTitle}</h2>
                <p>{t.projectsDescription}</p>
              </div>
              <Boxes size={18} />
            </div>
            <div className="project-list">
              {(projects.data ?? []).map((project) => (
                <button
                  className={project.id === selectedProjectId ? "project-row active" : "project-row"}
                  key={project.id}
                  onClick={() => setSelectedProjectId(project.id)}
                >
                  <strong>{project.name}</strong>
                  <span>{project.description}</span>
                </button>
              ))}
              {projects.isLoading && <div className="muted-row">{t.loadingProjects}</div>}
            </div>
          </div>

          <div className="panel">
            <div className="panel-header">
              <div>
                <h2>
                  {selectedProject?.name ?? t.projectFallback} {t.vmsTitleSuffix}
                </h2>
                <p>{t.vmsDescription}</p>
              </div>
            </div>
            <form className="create-form" onSubmit={handleCreateVM}>
              <input
                aria-label={t.vmNameAria}
                placeholder={t.vmNamePlaceholder}
                value={vmName}
                onChange={(event) => setVmName(event.target.value)}
              />
              <select
                aria-label={t.imageAria}
                value={selectedImageId}
                onChange={(event) => setSelectedImageId(event.target.value)}
              >
                {readyImages.length === 0 && <option value="">{t.noReadyImages}</option>}
                {(["public", "project"] as const).map((scope) => {
                  const group = readyImages.filter((image) => image.scope === scope);
                  return group.length === 0 ? null : (
                    <optgroup key={scope} label={scope === "public" ? t.imageScopePublic : t.imageScopeProject}>
                      {group.map((image) => (
                        <option key={image.id} value={image.id}>
                          {image.name}
                        </option>
                      ))}
                    </optgroup>
                  );
                })}
              </select>
              <select
                aria-label={t.flavorAria}
                value={selectedFlavorId}
                onChange={(event) => setSelectedFlavorId(event.target.value)}
              >
                {(flavors.data ?? []).map((flavor) => (
                  <option key={flavor.id} value={flavor.id} disabled={!flavorFitsImage(flavor)}>
                    {flavor.name}
                    {flavorFitsImage(flavor) ? "" : ` (${t.flavorDiskTooSmall})`}
                  </option>
                ))}
              </select>
              <select
                aria-label={t.vmNetworkLabel}
                value={vmNetworkId}
                onChange={(event) => {
                  setVmNetworkId(event.target.value);
                  setSelectedSubnetId("");
                }}
              >
                <option value="">{t.vmNetworkNone}</option>
                {(networks.data ?? []).map((network) => (
                  <option key={network.id} value={network.id}>
                    {network.name}
                    {network.cidr ? ` (${network.cidr})` : ""}
                  </option>
                ))}
              </select>
              <select
                aria-label={t.vmSubnetLabel}
                value={selectedSubnetId}
                disabled={vmNetworkId === ""}
                onChange={(event) => setSelectedSubnetId(event.target.value)}
              >
                <option value="">
                  {vmNetworkId === ""
                    ? t.vmSubnetNone
                    : (vmSubnets.data ?? []).length === 0 && !vmSubnets.isLoading
                      ? t.vmSubnetEmpty
                      : t.vmSubnetSelect}
                </option>
                {(vmSubnets.data ?? []).map((subnet) => (
                  <option key={subnet.id} value={subnet.id}>
                    {subnet.name} ({subnet.cidr}, {subnet.type === "public" ? t.subnetTypePublic : t.subnetTypePrivate})
                  </option>
                ))}
              </select>
              <input
                aria-label={t.cloudInitUsername}
                placeholder={t.cloudInitUsername}
                value={cloudInitUsername}
                onChange={(event) => setCloudInitUsername(event.target.value)}
              />
              <input
                aria-label={t.cloudInitPassword}
                placeholder={t.cloudInitPassword}
                type="password"
                value={cloudInitPassword}
                onChange={(event) => setCloudInitPassword(event.target.value)}
              />
              <input
                aria-label={t.cloudInitSSHPublicKey}
                className="wide-input"
                placeholder={t.cloudInitSSHPublicKey}
                value={cloudInitSSHPublicKey}
                onChange={(event) => setCloudInitSSHPublicKey(event.target.value)}
              />
              <button className="primary-action" disabled={!canCreateVM || createVM.isPending}>
                <Play size={16} />
                {createVM.isPending ? t.creating : t.createVM}
              </button>
            </form>
            {vmSubnetMissing && <div className="muted-row">{t.vmSubnetRequired}</div>}
            {createVM.isError && <div className="error-row">{translateApiError(createVM.error, language, t.createFailed)}</div>}
            {deleteVM.isError && <div className="error-row">{t.deleteFailed}</div>}
            {(startVM.isError || stopVM.isError) && <div className="error-row">{t.actionFailed}</div>}
            <div className="table">
              <div className="table-row table-head">
                <span>{t.columnName}</span>
                <span>{t.columnStatus}</span>
                <span>{t.columnImage}</span>
                <span>{t.columnFlavor}</span>
                <span>{t.columnActions}</span>
              </div>
              {(virtualMachines.data ?? []).map((vm) => (
                <div className={vm.id === selectedVMId ? "table-row selected" : "table-row"} key={vm.id}>
                  <strong>{vm.name}</strong>
                  <span className={`state state-${vm.status.toLowerCase()}`}>{translateStatus(vm.status, language)}</span>
                  <span>{vm.imageId}</span>
                  <span>{vm.flavorId}</span>
                  <div className="row-actions">
                    <button className="icon-action" onClick={() => setSelectedVMId(vm.id)}>
                      <Server size={16} />
                      <span>{t.details}</span>
                    </button>
                    <button
                      className="icon-action"
                      disabled={startVM.isPending && startVM.variables === vm.id}
                      onClick={() => startVM.mutate(vm.id)}
                    >
                      <Play size={16} />
                      <span>{t.startVM}</span>
                    </button>
                    <button
                      className="icon-action"
                      disabled={stopVM.isPending && stopVM.variables === vm.id}
                      onClick={() => stopVM.mutate(vm.id)}
                    >
                      <CircleStop size={16} />
                      <span>{t.stopVM}</span>
                    </button>
                    <button className="icon-action" disabled={vm.status !== "Running"} onClick={() => openConsole(vm.id)}>
                      <Terminal size={16} />
                      <span>{t.console}</span>
                    </button>
                    <button
                      className="icon-action danger-action"
                      title={t.deleteVM}
                      aria-label={`${t.deleteVM} ${vm.name}`}
                      disabled={deleteVM.isPending && deleteVM.variables === vm.id}
                      onClick={() => deleteVM.mutate(vm.id)}
                    >
                      <Trash2 size={16} />
                      <span>{deleteVM.isPending && deleteVM.variables === vm.id ? t.deleting : t.deleteVM}</span>
                    </button>
                  </div>
                </div>
              ))}
              {!isCatalogLoading && vmCount === 0 && <div className="muted-row">{t.noVirtualMachines}</div>}
            </div>
          </div>
        </section>

        <section className="panel vm-detail-panel">
          <div className="panel-header">
            <div>
              <h2>{t.vmDetailsTitle}</h2>
              <p>{t.vmDetailsDescription}</p>
            </div>
            <Server size={18} />
          </div>
          {selectedVMDetail.data ? (
            <div className="detail-content">
              <div className="detail-summary">
                <div>
                  <span>{t.columnName}</span>
                  <strong>{selectedVMDetail.data.virtualMachine.name}</strong>
                </div>
                <div>
                  <span>{t.runtimeStatus}</span>
                  <strong className={`state state-${selectedVMDetail.data.runtimeStatus.toLowerCase()}`}>
                    {translateStatus(selectedVMDetail.data.runtimeStatus, language)}
                  </strong>
                </div>
                <div>
                  <span>{t.failureReason}</span>
                  <strong>{selectedVMDetail.data.failureReason || "-"}</strong>
                </div>
                <div>
                  <span>{t.failureMessage}</span>
                  <strong>{selectedVMDetail.data.failureMessage || "-"}</strong>
                </div>
                <div>
                  <span>{t.vmiPhase}</span>
                  <strong>{selectedVMDetail.data.vmiPhase || "-"}</strong>
                </div>
                <div>
                  <span>{t.vmiIp}</span>
                  <strong>{selectedVMDetail.data.vmiIp || "-"}</strong>
                </div>
                <div>
                  <span>{t.nodeName}</span>
                  <strong>{selectedVMDetail.data.nodeName || "-"}</strong>
                </div>
                <div>
                  <span>{t.dataVolume}</span>
                  <strong>
                    {selectedVMDetail.data.dataVolumePhase || "-"} {selectedVMDetail.data.dataVolumeStatus}
                  </strong>
                </div>
                <div>
                  <span>{t.pvc}</span>
                  <strong>
                    {selectedVMDetail.data.pvcPhase || "-"} {selectedVMDetail.data.pvcCapacity}
                  </strong>
                </div>
                <div>
                  <span>{t.vmNetworkLabel}</span>
                  <strong>
                    {selectedVMDetail.data.network
                      ? `${selectedVMDetail.data.network.name}${selectedVMDetail.data.network.cidr ? ` (${selectedVMDetail.data.network.cidr})` : ""}`
                      : selectedVMDetail.data.virtualMachine.subnetId
                        ? "-"
                        : t.vmNetworkNone}
                  </strong>
                </div>
                <div>
                  <span>{t.vmSubnetLabel}</span>
                  <strong>
                    {selectedVMDetail.data.subnet
                      ? `${selectedVMDetail.data.subnet.name} (${selectedVMDetail.data.subnet.cidr}, ${
                          selectedVMDetail.data.subnet.type === "public" ? t.subnetTypePublic : t.subnetTypePrivate
                        })`
                      : selectedVMDetail.data.virtualMachine.subnetId || t.vmSubnetNone}
                  </strong>
                </div>
              </div>
              <div className="events-list">
                <h3>{t.recentEvents}</h3>
                {selectedVMDetail.data.events.length > 0 ? (
                  selectedVMDetail.data.events.map((event, index) => (
                    <div className="event-row" key={`${event.reason}-${index}`}>
                      <span className={`event-type event-${event.type.toLowerCase()}`}>{event.type}</span>
                      <strong>{event.reason}</strong>
                      <p>{event.message}</p>
                    </div>
                  ))
                ) : (
                  <div className="muted-row">{t.noEvents}</div>
                )}
              </div>
            </div>
          ) : (
            <div className="muted-row">{selectedVMId ? t.loadingProjects : t.selectVM}</div>
          )}
        </section>

        <section className="split-layout">
          <div className="panel">
            <div className="panel-header">
              <div>
                <h2>{t.networksTitle}</h2>
              </div>
              <Network size={18} />
            </div>
            <form className="create-form" onSubmit={handleCreateNetwork}>
              <input
                aria-label={t.networkNameLabel}
                placeholder={t.networkNameLabel}
                value={newNetworkName}
                onChange={(event) => setNewNetworkName(event.target.value)}
              />
              <input
                aria-label={t.networkCidrLabel}
                placeholder={t.networkCidrLabel}
                value={newNetworkCidr}
                onChange={(event) => setNewNetworkCidr(event.target.value)}
              />
              <button className="primary-action" disabled={!canCreateNetwork || createNetworkMutation.isPending}>
                <Play size={16} />
                {createNetworkMutation.isPending ? t.creating : t.networkCreate}
              </button>
            </form>
            {createNetworkMutation.isError && (
              <div className="error-row">
                {translateApiError(createNetworkMutation.error, language, t.networkActionFailed)}
              </div>
            )}
            {deleteNetworkMutation.isError && (
              <div className="error-row">
                {translateApiError(deleteNetworkMutation.error, language, t.networkActionFailed)}
              </div>
            )}
            <div className="compact-list">
              {(networks.data ?? []).map((network) => (
                <div
                  className={network.id === selectedNetworkId ? "compact-row selected" : "compact-row"}
                  key={network.id}
                >
                  <button
                    className="project-row"
                    style={{ flex: 1, textAlign: "left" }}
                    onClick={() => setSelectedNetworkId(network.id)}
                  >
                    <strong>{network.name}</strong>
                    {network.cidr && <span>{network.cidr}</span>}
                    <span className={`state state-${network.status.toLowerCase()}`}>{network.status}</span>
                  </button>
                  <button
                    className="icon-action danger-action"
                    aria-label={`${t.networkDelete} ${network.name}`}
                    disabled={deleteNetworkMutation.isPending && deleteNetworkMutation.variables === network.id}
                    onClick={() => deleteNetworkMutation.mutate(network.id)}
                  >
                    <Trash2 size={16} />
                    <span>{t.networkDelete}</span>
                  </button>
                </div>
              ))}
              {networks.isLoading && <div className="muted-row">{t.statusChecking}</div>}
              {!networks.isLoading && (networks.data ?? []).length === 0 && (
                <div className="muted-row">{t.networkEmpty}</div>
              )}
            </div>
          </div>

          <div className="panel">
            <div className="panel-header">
              <div>
                <h2>{t.subnetsTitle}</h2>
              </div>
            </div>
            {selectedNetworkId !== "" ? (
              <>
                <form className="create-form" onSubmit={handleCreateSubnet}>
                  <input
                    aria-label={t.subnetNameLabel}
                    placeholder={t.subnetNameLabel}
                    value={newSubnetName}
                    onChange={(event) => setNewSubnetName(event.target.value)}
                  />
                  <input
                    aria-label={t.subnetCidrLabel}
                    placeholder={subnetCidrPlaceholder}
                    value={newSubnetCidr}
                    onChange={(event) => setNewSubnetCidr(event.target.value)}
                  />
                  <select
                    aria-label={t.subnetTypeLabel}
                    value={newSubnetType}
                    onChange={(event) => setNewSubnetType(event.target.value as "private" | "public")}
                  >
                    <option value="private">{t.subnetTypePrivate}</option>
                    <option value="public">{t.subnetTypePublic}</option>
                  </select>
                  <button className="primary-action" disabled={!canCreateSubnet || createSubnetMutation.isPending}>
                    <Play size={16} />
                    {createSubnetMutation.isPending ? t.creating : t.subnetCreate}
                  </button>
                </form>
                {createSubnetMutation.isError && (
                  <div className="error-row">
                    {translateApiError(createSubnetMutation.error, language, t.subnetActionFailed)}
                  </div>
                )}
                {deleteSubnetMutation.isError && (
                  <div className="error-row">
                    {translateApiError(deleteSubnetMutation.error, language, t.subnetActionFailed)}
                  </div>
                )}
                <div className="table">
                  <div className="table-row table-head">
                    <span>{t.subnetNameLabel}</span>
                    <span>{t.subnetCidrLabel}</span>
                    <span>{t.subnetTypeLabel}</span>
                    <span>{t.columnActions}</span>
                  </div>
                  {(subnets.data ?? []).map((subnet) => (
                    <div className="table-row" key={subnet.id}>
                      <strong>{subnet.name}</strong>
                      <span>{subnet.cidr}</span>
                      <span>{subnet.type}</span>
                      <div className="row-actions">
                        <button
                          className="icon-action danger-action"
                          aria-label={`${t.networkDelete} ${subnet.name}`}
                          disabled={deleteSubnetMutation.isPending && deleteSubnetMutation.variables === subnet.id}
                          onClick={() => deleteSubnetMutation.mutate(subnet.id)}
                        >
                          <Trash2 size={16} />
                          <span>{t.networkDelete}</span>
                        </button>
                      </div>
                    </div>
                  ))}
                  {subnets.isLoading && <div className="muted-row">{t.statusChecking}</div>}
                  {!subnets.isLoading && (subnets.data ?? []).length === 0 && (
                    <div className="muted-row">{t.subnetEmpty}</div>
                  )}
                </div>
              </>
            ) : (
              <div className="muted-row">{t.networkEmpty}</div>
            )}
          </div>
        </section>

        <section className="detail-grid">
          <div className="panel">
            <div className="panel-header">
              <div>
                <h2>{t.imagesTitle}</h2>
                <p>{t.imagesDescription}</p>
              </div>
              <Image size={18} />
            </div>
            <form className="create-form image-form" onSubmit={handleCreateImage}>
              <select aria-label={t.imagePreset} value="" onChange={(event) => applyImagePreset(event.target.value)}>
                <option value="">{t.imagePreset}</option>
                {imagePresets.map((preset) => (
                  <option key={preset.name} value={preset.name}>
                    {preset.name} ({preset.minFlavor}+)
                  </option>
                ))}
              </select>
              <input
                aria-label={t.imageNameLabel}
                placeholder={t.imageNameLabel}
                value={newImageName}
                onChange={(event) => setNewImageName(event.target.value)}
              />
              <select
                aria-label={t.imageOsTypeLabel}
                value={newImageOsType}
                onChange={(event) => setNewImageOsType(event.target.value as "linux" | "windows")}
              >
                <option value="linux">Linux</option>
                <option value="windows">Windows</option>
              </select>
              <input
                className="wide-input"
                aria-label={t.imageUrlLabel}
                placeholder={t.imageUrlLabel}
                value={newImageUrl}
                onChange={(event) => setNewImageUrl(event.target.value)}
              />
              <button className="primary-action" disabled={!canCreateImage || createImageMutation.isPending}>
                <Play size={16} />
                {createImageMutation.isPending ? t.creating : t.imageRegister}
              </button>
            </form>
            {createImageMutation.isError && (
              <div className="error-row">{translateApiError(createImageMutation.error, language, t.imageActionFailed)}</div>
            )}
            {deleteImageMutation.isError && (
              <div className="error-row">{translateApiError(deleteImageMutation.error, language, t.imageActionFailed)}</div>
            )}
            <div className="compact-list">
              {(["project", "public"] as const).map((scope) => {
                const group = (images.data ?? []).filter((image) => image.scope === scope);
                return (
                  <div key={scope}>
                    <div className="list-group-label">{scope === "public" ? t.imageScopePublic : t.imageScopeProject}</div>
                    {group.map((image) => (
                      <div className="compact-row" key={image.id}>
                        <div>
                          <strong>
                            {image.name}{" "}
                            <small>
                              {image.osType} ·{" "}
                              {image.virtualSizeBytes
                                ? t.imageDiskSize.replace("{size}", formatGiBPrecise(image.virtualSizeBytes))
                                : t.imageDiskSizeUnknown}
                            </small>
                          </strong>
                          <span>{image.sourceUrl}</span>
                        </div>
                        <div className="row-actions">
                          <span className={`state state-${image.status.toLowerCase()}`}>
                            {translateStatus(image.status, language)}
                          </span>
                          {scope === "public" ? (
                            <span className="state">{t.imagePublicBadge}</span>
                          ) : (
                            <button
                              type="button"
                              className="icon-action danger-action"
                              aria-label={`${t.imageDelete} ${image.name}`}
                              disabled={deleteImageMutation.isPending && deleteImageMutation.variables === image.id}
                              onClick={() => deleteImageMutation.mutate(image.id)}
                            >
                              <Trash2 size={16} />
                              <span>{t.imageDelete}</span>
                            </button>
                          )}
                        </div>
                      </div>
                    ))}
                    {!isCatalogLoading && group.length === 0 && (
                      <div className="muted-row">{scope === "public" ? t.publicImagesEmpty : t.projectImagesEmpty}</div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>

          <div className="panel">
            <div className="panel-header">
              <div>
                <h2>{t.flavorsTitle}</h2>
                <p>{t.flavorsDescription}</p>
              </div>
              <Cpu size={18} />
            </div>
            <div className="flavor-grid">
              {(flavors.data ?? []).map((flavor) => (
                <div className="flavor" key={flavor.id}>
                  <strong>{flavor.name}</strong>
                  <span>
                    {flavor.cpu} {t.vcpu}
                  </span>
                  <span>
                    {formatGiB(flavor.memoryBytes)} {t.ram}
                  </span>
                  <span>
                    {formatGiB(flavor.rootDiskBytes)} {t.disk}
                  </span>
                </div>
              ))}
              {flavors.isLoading && <div className="muted-row">{t.loadingFlavors}</div>}
            </div>
          </div>
        </section>
        {consoleVMId !== "" && (
          <section className="console-overlay" aria-label={t.console}>
            <div className="console-panel">
              <div className="console-header">
                <div>
                  <strong>{consoleVMId}</strong>
                  <span>
                    {consoleState === "connecting"
                      ? t.consoleConnecting
                      : consoleState === "connected"
                        ? t.consoleConnected
                        : consoleState === "error"
                          ? t.consoleUnavailable
                          : t.consoleDisconnected}
                  </span>
                  <span>{t.consoleLoginHint}</span>
                </div>
                <button className="icon-action" onClick={closeConsole}>
                  <CircleStop size={16} />
                  <span>{t.consoleClose}</span>
                </button>
              </div>
              <div
                className="console-terminal"
                ref={consoleTerminalRef}
                tabIndex={0}
                onKeyDown={handleConsoleKeyDown}
                onPaste={handleConsolePaste}
              >
                <pre>{consoleOutput || t.consoleFocusHint}</pre>
              </div>
            </div>
          </section>
        )}
      </main>
    </div>
  );
}

function formatGiB(bytes: number): number {
  return Math.round(bytes / 1024 / 1024 / 1024);
}

function formatGiBPrecise(bytes: number): string {
  return (bytes / 1024 / 1024 / 1024).toFixed(1);
}

async function decodeConsoleMessage(data: MessageEvent["data"]): Promise<string> {
  let text = "";
  if (typeof data === "string") {
    text = data;
  } else if (data instanceof Blob) {
    text = await data.text();
  } else if (data instanceof ArrayBuffer) {
    text = new TextDecoder().decode(data);
  }
  return text.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
}

function consoleKeyValue(event: KeyboardEvent<HTMLDivElement>): string {
  // Ctrl+letter sends the terminal control character (Ctrl+C = \x03 interrupts, Ctrl+D = \x04 ends input).
  // Cmd/Alt combinations are left to the browser so copy and paste keep working.
  // The physical key (event.code) is used, not event.key: with a Korean or other IME active, Ctrl+C reports
  // event.key as "ㅊ" or "Process".
  if (event.ctrlKey && !event.metaKey && !event.altKey) {
    const letter = /^Key([A-Z])$/.exec(event.code);
    if (letter) {
      return String.fromCharCode(letter[1].charCodeAt(0) - 64);
    }
    const controlSymbols: Record<string, string> = {
      BracketLeft: "\x1b",
      Backslash: "\x1c",
      BracketRight: "\x1d",
      Space: "\x00",
    };
    return controlSymbols[event.code] ?? "";
  }
  if (event.key.length === 1) {
    return event.key;
  }
  switch (event.key) {
    case "Enter":
      return "\r";
    case "Backspace":
      return "\x7f";
    case "Tab":
      return "\t";
    case "Escape":
      return "\x1b";
    case "ArrowUp":
      return "\x1b[A";
    case "ArrowDown":
      return "\x1b[B";
    case "ArrowRight":
      return "\x1b[C";
    case "ArrowLeft":
      return "\x1b[D";
    case "Home":
      return "\x1b[H";
    case "End":
      return "\x1b[F";
    case "Delete":
      return "\x1b[3~";
    default:
      return "";
  }
}
