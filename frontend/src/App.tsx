import { Activity, Boxes, CircleStop, Cpu, HardDrive, Image, Network, Play, Server, Terminal, Trash2 } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { FormEvent, KeyboardEvent } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  createNetwork,
  createSubnet,
  createVirtualMachine,
  deleteNetwork,
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
} from "./api";
import { getInitialLanguage, languageOptions, messages, saveLanguage, translateStatus } from "./i18n";
import type { Language } from "./i18n";

const navItems = [
  { labelKey: "navProjects", icon: Boxes },
  { labelKey: "navVirtualMachines", icon: Server },
  { labelKey: "navImages", icon: Image },
  { labelKey: "navStorage", icon: HardDrive },
] as const;

export function App() {
  const [language, setLanguage] = useState<Language>(() => getInitialLanguage());
  const [selectedProjectId, setSelectedProjectId] = useState<string>("");
  const [vmName, setVmName] = useState("");
  const [selectedImageId, setSelectedImageId] = useState("");
  const [selectedFlavorId, setSelectedFlavorId] = useState("");
  const [selectedSubnetId, setSelectedSubnetId] = useState("");
  const [selectedVMId, setSelectedVMId] = useState("");
  const [consoleVMId, setConsoleVMId] = useState("");
  const [consoleOutput, setConsoleOutput] = useState("");
  const [consoleState, setConsoleState] = useState<"idle" | "connecting" | "connected" | "disconnected" | "error">("idle");
  const consoleSocket = useRef<WebSocket | null>(null);
  const [selectedNetworkId, setSelectedNetworkId] = useState("");
  const [newNetworkName, setNewNetworkName] = useState("");
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
      }),
    onSuccess: () => {
      setVmName("");
      setSelectedSubnetId("");
      void queryClient.invalidateQueries({ queryKey: ["vms", selectedProjectId] });
      void queryClient.invalidateQueries({ queryKey: ["vm-detail", selectedProjectId] });
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
  const subnets = useQuery({
    queryKey: ["subnets", selectedProjectId, selectedNetworkId],
    queryFn: () => fetchSubnets(selectedProjectId, selectedNetworkId),
    enabled: selectedProjectId !== "" && selectedNetworkId !== "",
  });
  const createNetworkMutation = useMutation({
    mutationFn: () => createNetwork(selectedProjectId, newNetworkName.trim()),
    onSuccess: () => {
      setNewNetworkName("");
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
    if (!selectedProjectId && projects.data && projects.data.length > 0) {
      setSelectedProjectId(projects.data[0].id);
    }
  }, [projects.data, selectedProjectId]);

  useEffect(() => {
    if (images.data && images.data.length > 0 && !images.data.some((image) => image.id === selectedImageId)) {
      setSelectedImageId(images.data[0].id);
    }
  }, [images.data, selectedImageId]);

  useEffect(() => {
    if (flavors.data && flavors.data.length > 0 && !flavors.data.some((flavor) => flavor.id === selectedFlavorId)) {
      setSelectedFlavorId(flavors.data[0].id);
    }
  }, [flavors.data, selectedFlavorId]);

  useEffect(() => {
    if (virtualMachines.data && virtualMachines.data.length > 0 && !virtualMachines.data.some((vm) => vm.id === selectedVMId)) {
      setSelectedVMId(virtualMachines.data[0].id);
    }
  }, [selectedVMId, virtualMachines.data]);

  useEffect(() => {
    setSelectedNetworkId("");
  }, [selectedProjectId]);

  const isCatalogLoading = projects.isLoading || flavors.isLoading || images.isLoading || virtualMachines.isLoading;
  const projectCount = projects.data?.length ?? 0;
  const vmCount = virtualMachines.data?.length ?? 0;
  const imageCount = images.data?.length ?? 0;
  const canCreateVM = selectedProjectId !== "" && vmName.trim() !== "" && selectedImageId !== "" && selectedFlavorId !== "";
  const canCreateNetwork = selectedProjectId !== "" && newNetworkName.trim() !== "";
  const canCreateSubnet = selectedProjectId !== "" && selectedNetworkId !== "" && newSubnetName.trim() !== "" && newSubnetCidr.trim() !== "";
  const healthStatus = health.data?.status ?? (health.isLoading ? "checking" : "offline");

  function handleCreateVM(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (canCreateVM) {
      createVM.mutate();
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
      setConsoleOutput((current) => `${current}\n${t.consoleDisconnected}\n`);
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
                {(images.data ?? []).map((image) => (
                  <option key={image.id} value={image.id}>
                    {image.name}
                  </option>
                ))}
              </select>
              <select
                aria-label={t.flavorAria}
                value={selectedFlavorId}
                onChange={(event) => setSelectedFlavorId(event.target.value)}
              >
                {(flavors.data ?? []).map((flavor) => (
                  <option key={flavor.id} value={flavor.id}>
                    {flavor.name}
                  </option>
                ))}
              </select>
              <select
                aria-label={t.vmSubnetLabel}
                value={selectedSubnetId}
                onChange={(event) => setSelectedSubnetId(event.target.value)}
              >
                <option value="">{t.vmSubnetNone}</option>
                {(subnets.data ?? []).map((subnet) => (
                  <option key={subnet.id} value={subnet.id}>
                    {subnet.name} ({subnet.cidr})
                  </option>
                ))}
              </select>
              <button className="primary-action" disabled={!canCreateVM || createVM.isPending}>
                <Play size={16} />
                {createVM.isPending ? t.creating : t.createVM}
              </button>
            </form>
            {createVM.isError && <div className="error-row">{t.createFailed}</div>}
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
                    <button className="icon-action" onClick={() => openConsole(vm.id)}>
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
                  <span>{t.vmSubnetLabel}</span>
                  <strong>{selectedVMDetail.data.virtualMachine.subnetId || t.vmSubnetNone}</strong>
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
              <button className="primary-action" disabled={!canCreateNetwork || createNetworkMutation.isPending}>
                <Play size={16} />
                {createNetworkMutation.isPending ? t.creating : t.networkCreate}
              </button>
            </form>
            {createNetworkMutation.isError && <div className="error-row">{t.networkActionFailed}</div>}
            {deleteNetworkMutation.isError && <div className="error-row">{t.networkActionFailed}</div>}
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
                    placeholder={t.subnetCidrLabel}
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
                {createSubnetMutation.isError && <div className="error-row">{t.subnetActionFailed}</div>}
                {deleteSubnetMutation.isError && <div className="error-row">{t.subnetActionFailed}</div>}
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
            <div className="compact-list">
              {(images.data ?? []).map((image) => (
                <div className="compact-row" key={image.id}>
                  <div>
                    <strong>{image.name}</strong>
                    <span>{image.sourceUrl}</span>
                  </div>
                  <span className={`state state-${image.status.toLowerCase()}`}>
                    {translateStatus(image.status, language)}
                  </span>
                </div>
              ))}
              {!isCatalogLoading && imageCount === 0 && <div className="muted-row">{t.noImages}</div>}
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
                        : t.consoleDisconnected}
                  </span>
                </div>
                <button className="icon-action" onClick={closeConsole}>
                  <CircleStop size={16} />
                  <span>{t.consoleClose}</span>
                </button>
              </div>
              <div className="console-terminal" tabIndex={0} onKeyDown={handleConsoleKeyDown}>
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

async function decodeConsoleMessage(data: MessageEvent["data"]): Promise<string> {
  if (typeof data === "string") {
    return data;
  }
  if (data instanceof Blob) {
    return data.text();
  }
  if (data instanceof ArrayBuffer) {
    return new TextDecoder().decode(data);
  }
  return "";
}

function consoleKeyValue(event: KeyboardEvent<HTMLDivElement>): string {
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
    default:
      return "";
  }
}
