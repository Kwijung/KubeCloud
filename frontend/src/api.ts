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
  vmCount?: number; // admin listing only
};

export type FlavorInput = {
  name: string;
  cpu: number;
  memoryBytes: number;
  rootDiskBytes: number;
};

export type Network = {
  id: string;
  projectId: string;
  name: string;
  cidr: string; // empty for VPCs created before VPC CIDRs existed
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
  type: "private" | "public";
  status: string;
  createdAt: string;
};

export type Image = {
  id: string;
  projectId: string;
  name: string;
  osType: string;
  sourceUrl: string;
  status: string; // Importing | Ready | Failed
  createdAt: string;
  scope: "public" | "project"; // public images come from an admin and every project can use them
  virtualSizeBytes?: number; // missing when the size could not be read (non-qcow2 image)
  minRootDiskBytes?: number; // smallest flavor root disk that holds the image
  vmCount?: number; // admin listing only
  cacheStatus?: string;
  statusMessage?: string; // why the import failed
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
  subnet?: Subnet;
  network?: Network;
  events: RuntimeEvent[];
};

export type ConsoleCapability = {
  available: boolean;
  inUse: boolean; // another browser or client holds the serial console
  message: string;
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
  cloudInit?: {
    username?: string;
    password?: string;
    sshPublicKey?: string;
  };
};

export type CreateImageRequest = {
  name: string;
  osType: string;
  sourceUrl: string;
};

// Same origin by default: nginx (deployed) and the Vite dev server (npm run dev) proxy /api to the API.
const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "";

// ApiError carries the backend's error code (e.g. SUBNET_CIDR_OVERLAP) so the UI can explain the failure.
export class ApiError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`${apiBaseUrl}/api/v1${path}`, {
      method,
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch (error) {
    throw new ApiError("NETWORK_ERROR", 0, error instanceof Error ? error.message : "network error");
  }
  if (!response.ok) {
    let code = "";
    let message = `${method} ${path}: ${response.status}`;
    try {
      const payload = (await response.json()) as { error?: { code?: string; message?: string } };
      code = payload.error?.code ?? "";
      message = payload.error?.message ?? message;
    } catch {
      // Not a JSON error body (e.g. a proxy error page).
    }
    throw new ApiError(code, response.status, message);
  }
  if (response.status === 204 || response.status === 202) {
    return undefined as T;
  }
  const text = await response.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

async function data<T>(method: string, path: string, body?: unknown): Promise<T> {
  const payload = await request<{ data: T }>(method, path, body);
  return payload.data;
}

const vmPath = (projectId: string, vmId: string) => `/projects/${projectId}/vms/${vmId}`;

export const api = {
  health: () => request<HealthResponse>("GET", "/health"),
  preflight: () => data<PreflightResult>("GET", "/preflight"),
  projects: () => data<Project[]>("GET", "/projects"),
  flavors: () => data<Flavor[]>("GET", "/flavors"),

  networks: (projectId: string) => data<Network[]>("GET", `/projects/${projectId}/networks`),
  createNetwork: (projectId: string, name: string, cidr: string) =>
    data<Network>("POST", `/projects/${projectId}/networks`, { name, cidr }),
  deleteNetwork: (projectId: string, networkId: string) =>
    request<void>("DELETE", `/projects/${projectId}/networks/${networkId}`),
  subnets: (projectId: string, networkId: string) =>
    data<Subnet[]>("GET", `/projects/${projectId}/networks/${networkId}/subnets`),
  createSubnet: (projectId: string, networkId: string, subnet: { name: string; cidr: string; type: string }) =>
    data<Subnet>("POST", `/projects/${projectId}/networks/${networkId}/subnets`, subnet),
  deleteSubnet: (projectId: string, networkId: string, subnetId: string) =>
    request<void>("DELETE", `/projects/${projectId}/networks/${networkId}/subnets/${subnetId}`),

  images: (projectId: string) => data<Image[]>("GET", `/projects/${projectId}/images`),
  createImage: (projectId: string, image: CreateImageRequest) => data<Image>("POST", `/projects/${projectId}/images`, image),
  deleteImage: (projectId: string, imageId: string) => request<void>("DELETE", `/projects/${projectId}/images/${imageId}`),

  vms: (projectId: string) => data<VirtualMachine[]>("GET", `/projects/${projectId}/vms`),
  vm: (projectId: string, vmId: string) => data<VirtualMachineDetail>("GET", vmPath(projectId, vmId)),
  createVM: (projectId: string, vm: CreateVirtualMachineRequest) =>
    data<VirtualMachine>("POST", `/projects/${projectId}/vms`, vm),
  deleteVM: (projectId: string, vmId: string) => request<void>("DELETE", vmPath(projectId, vmId)),
  startVM: (projectId: string, vmId: string) => request<void>("POST", `${vmPath(projectId, vmId)}/start`),
  stopVM: (projectId: string, vmId: string) => request<void>("POST", `${vmPath(projectId, vmId)}/stop`),
  restartVM: (projectId: string, vmId: string) => request<void>("POST", `${vmPath(projectId, vmId)}/restart`),
  console: (projectId: string, vmId: string) => data<ConsoleCapability>("GET", `${vmPath(projectId, vmId)}/console`),
  bootLog: (projectId: string, vmId: string) => data<{ log: string }>("GET", `${vmPath(projectId, vmId)}/console/log`),

  admin: {
    flavors: () => data<Flavor[]>("GET", "/admin/flavors"),
    createFlavor: (flavor: FlavorInput) => data<Flavor>("POST", "/admin/flavors", flavor),
    updateFlavor: (id: string, update: Partial<FlavorInput> & { enabled?: boolean }) =>
      data<Flavor>("PATCH", `/admin/flavors/${id}`, update),
    deleteFlavor: (id: string) => request<void>("DELETE", `/admin/flavors/${id}`),
    images: () => data<Image[]>("GET", "/admin/images"),
    createImage: (image: CreateImageRequest) => data<Image>("POST", "/admin/images", image),
    deleteImage: (id: string) => request<void>("DELETE", `/admin/images/${id}`),
  },
};

export function consoleStreamURL(projectId: string, vmId: string): string {
  const base = new URL(apiBaseUrl || window.location.origin, window.location.origin);
  base.protocol = base.protocol === "https:" ? "wss:" : "ws:";
  base.pathname = `/api/v1${vmPath(projectId, vmId)}/console/stream`;
  base.search = "";
  base.hash = "";
  return base.toString();
}

// The API closes a console session with this code when another session takes the console over.
export const CONSOLE_REPLACED_CLOSE_CODE = 4001;
