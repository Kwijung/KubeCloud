export type HealthResponse = {
  service: string;
  status: string;
  version: string;
};

export type Project = {
  id: string;
  name: string;
  description: string;
  createdAt: string;
};

export type Flavor = {
  id: string;
  name: string;
  cpu: number;
  memoryBytes: number;
  rootDiskBytes: number;
  enabled: boolean;
};

export type Network = {
  id: string;
  projectId: string;
  name: string;
  status: string;
  createdAt: string;
};

export type Subnet = {
  id: string;
  networkId: string;
  projectId: string;
  name: string;
  cidr: string;
  gateway: string;
  type: string; // "private" | "public"
  status: string;
  createdAt: string;
};

export type Image = {
  id: string;
  projectId: string;
  name: string;
  osType: string;
  sourceUrl: string;
  status: string;
  createdAt: string;
};

export type VirtualMachine = {
  id: string;
  projectId: string;
  name: string;
  imageId: string;
  flavorId: string;
  status: string;
  createdAt: string;
  subnetId?: string;
};

export type RuntimeEvent = {
  type: string;
  reason: string;
  message: string;
};

export type VirtualMachineDetail = {
  virtualMachine: VirtualMachine;
  runtimeStatus: string;
  failureReason: string;
  failureMessage: string;
  vmiPhase: string;
  vmiIp: string;
  nodeName: string;
  dataVolumePhase: string;
  dataVolumeStatus: string;
  pvcPhase: string;
  pvcCapacity: string;
  consoleAvailable: boolean;
  events: RuntimeEvent[];
};

export type PreflightCheck = {
  name: string;
  status: string;
  message: string;
};

export type PreflightResult = {
  status: string;
  checks: PreflightCheck[];
};

export type CreateVirtualMachineRequest = {
  name: string;
  imageId: string;
  flavorId: string;
  subnetId?: string;
};

type ListResponse<T> = {
  data: T[];
};

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

export async function fetchHealth(): Promise<HealthResponse> {
  return getJSON<HealthResponse>("/api/v1/health");
}

export async function fetchProjects(): Promise<Project[]> {
  const response = await getJSON<ListResponse<Project>>("/api/v1/projects");
  return response.data;
}

export async function fetchFlavors(): Promise<Flavor[]> {
  const response = await getJSON<ListResponse<Flavor>>("/api/v1/flavors");
  return response.data;
}

export async function fetchNetworks(projectId: string): Promise<Network[]> {
  const response = await getJSON<ListResponse<Network>>(`/api/v1/projects/${projectId}/networks`);
  return response.data;
}

export async function createNetwork(projectId: string, name: string): Promise<Network> {
  const response = await fetch(`${apiBaseUrl}/api/v1/projects/${projectId}/networks`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
  if (!response.ok) {
    throw new Error(`network create failed: ${response.status}`);
  }
  const body = (await response.json()) as { data: Network };
  return body.data;
}

export async function deleteNetwork(projectId: string, networkId: string): Promise<void> {
  const response = await fetch(`${apiBaseUrl}/api/v1/projects/${projectId}/networks/${networkId}`, {
    method: "DELETE",
  });
  if (!response.ok) {
    throw new Error(`network delete failed: ${response.status}`);
  }
}

export async function fetchSubnets(projectId: string, networkId: string): Promise<Subnet[]> {
  const response = await getJSON<ListResponse<Subnet>>(
    `/api/v1/projects/${projectId}/networks/${networkId}/subnets`,
  );
  return response.data;
}

export async function createSubnet(
  projectId: string,
  networkId: string,
  request: { name: string; cidr: string; type: string; gateway?: string },
): Promise<Subnet> {
  const response = await fetch(
    `${apiBaseUrl}/api/v1/projects/${projectId}/networks/${networkId}/subnets`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(request),
    },
  );
  if (!response.ok) {
    throw new Error(`subnet create failed: ${response.status}`);
  }
  const body = (await response.json()) as { data: Subnet };
  return body.data;
}

export async function deleteSubnet(projectId: string, networkId: string, subnetId: string): Promise<void> {
  const response = await fetch(
    `${apiBaseUrl}/api/v1/projects/${projectId}/networks/${networkId}/subnets/${subnetId}`,
    { method: "DELETE" },
  );
  if (!response.ok) {
    throw new Error(`subnet delete failed: ${response.status}`);
  }
}

export async function fetchImages(projectId: string): Promise<Image[]> {
  const response = await getJSON<ListResponse<Image>>(`/api/v1/projects/${projectId}/images`);
  return response.data;
}

export async function fetchVirtualMachines(projectId: string): Promise<VirtualMachine[]> {
  const response = await getJSON<ListResponse<VirtualMachine>>(`/api/v1/projects/${projectId}/vms`);
  return response.data;
}

export async function fetchVirtualMachineDetail(projectId: string, vmId: string): Promise<VirtualMachineDetail> {
  const response = await getJSON<{ data: VirtualMachineDetail }>(`/api/v1/projects/${projectId}/vms/${vmId}`);
  return response.data;
}

export async function fetchPreflight(): Promise<PreflightResult> {
  const response = await getJSON<{ data: PreflightResult }>("/api/v1/preflight");
  return response.data;
}

export async function createVirtualMachine(projectId: string, request: CreateVirtualMachineRequest): Promise<VirtualMachine> {
  const response = await fetch(`${apiBaseUrl}/api/v1/projects/${projectId}/vms`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    throw new Error(`vm create failed: ${response.status}`);
  }
  const body = (await response.json()) as { data: VirtualMachine };
  return body.data;
}

export async function deleteVirtualMachine(projectId: string, vmId: string): Promise<void> {
  const response = await fetch(`${apiBaseUrl}/api/v1/projects/${projectId}/vms/${vmId}`, {
    method: "DELETE",
  });
  if (!response.ok) {
    throw new Error(`vm delete failed: ${response.status}`);
  }
}

export async function startVirtualMachine(projectId: string, vmId: string): Promise<void> {
  await postAction(`/api/v1/projects/${projectId}/vms/${vmId}/start`);
}

export async function stopVirtualMachine(projectId: string, vmId: string): Promise<void> {
  await postAction(`/api/v1/projects/${projectId}/vms/${vmId}/stop`);
}

export async function fetchConsoleCapability(projectId: string, vmId: string): Promise<{ available: boolean; message: string }> {
  const response = await getJSON<{ data: { available: boolean; message: string } }>(
    `/api/v1/projects/${projectId}/vms/${vmId}/console`,
  );
  return response.data;
}

export function virtualMachineConsoleStreamURL(projectId: string, vmId: string): string {
  const base = new URL(apiBaseUrl, window.location.origin);
  base.protocol = base.protocol === "https:" ? "wss:" : "ws:";
  base.pathname = `/api/v1/projects/${projectId}/vms/${vmId}/console/stream`;
  base.search = "";
  base.hash = "";
  return base.toString();
}

async function postAction(path: string): Promise<void> {
  const response = await fetch(`${apiBaseUrl}${path}`, { method: "POST" });
  if (!response.ok) {
    throw new Error(`api action failed: ${response.status}`);
  }
}

async function getJSON<T>(path: string): Promise<T> {
  const response = await fetch(`${apiBaseUrl}${path}`);
  if (!response.ok) {
    throw new Error(`api request failed: ${response.status}`);
  }
  return response.json();
}
