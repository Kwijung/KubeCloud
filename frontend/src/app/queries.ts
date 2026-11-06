import { useQueries, useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { Flavor, Image, Network, Subnet, VirtualMachine } from "../api";

// How often lists that reflect cluster state are refreshed.
export const LIST_REFRESH_MS = 15000;
// Faster refresh while something is changing (VM starting, image importing).
export const BUSY_REFRESH_MS = 4000;

export const keys = {
  projects: ["projects"] as const,
  flavors: ["flavors"] as const,
  allFlavors: ["flavors", "all"] as const,
  health: ["health"] as const,
  preflight: ["preflight"] as const,
  vms: (projectId: string) => ["vms", projectId] as const,
  vm: (projectId: string, vmId: string) => ["vm", projectId, vmId] as const,
  images: (projectId: string) => ["images", projectId] as const,
  networks: (projectId: string) => ["networks", projectId] as const,
  subnets: (projectId: string, networkId: string) => ["subnets", projectId, networkId] as const,
  adminFlavors: ["admin", "flavors"] as const,
  adminImages: ["admin", "images"] as const,
};

const busyVMStatuses = new Set(["Provisioning", "Importing", "Starting", "Stopping"]);

export function isVMBusy(status: string) {
  return busyVMStatuses.has(status);
}

export function useProjects() {
  return useQuery({ queryKey: keys.projects, queryFn: api.projects, staleTime: 60000 });
}

export function useFlavors() {
  return useQuery({ queryKey: keys.flavors, queryFn: api.flavors, staleTime: 30000 });
}

// useAllFlavors includes hidden flavors: use it to describe existing VMs, and useFlavors to create new ones.
export function useAllFlavors() {
  return useQuery({ queryKey: keys.allFlavors, queryFn: api.allFlavors, staleTime: 30000 });
}

export function useVMs(projectId: string) {
  return useQuery({
    queryKey: keys.vms(projectId),
    queryFn: () => api.vms(projectId),
    enabled: projectId !== "",
    refetchInterval: (query) =>
      (query.state.data ?? []).some((vm: VirtualMachine) => isVMBusy(vm.status)) ? BUSY_REFRESH_MS : LIST_REFRESH_MS,
  });
}

export function useImages(projectId: string) {
  return useQuery({
    queryKey: keys.images(projectId),
    queryFn: () => api.images(projectId),
    enabled: projectId !== "",
    refetchInterval: (query) =>
      (query.state.data ?? []).some((image: Image) => image.status === "Importing") ? BUSY_REFRESH_MS : false,
  });
}

export function useNetworks(projectId: string) {
  return useQuery({
    queryKey: keys.networks(projectId),
    queryFn: () => api.networks(projectId),
    enabled: projectId !== "",
  });
}

// useAllSubnets loads the subnets of every VPC of a project, keyed by VPC ID.
export function useAllSubnets(projectId: string, networks: Network[] | undefined) {
  const results = useQueries({
    queries: (networks ?? []).map((network) => ({
      queryKey: keys.subnets(projectId, network.id),
      queryFn: () => api.subnets(projectId, network.id),
    })),
  });
  const byNetwork = new Map<string, Subnet[]>();
  const byId = new Map<string, Subnet>();
  (networks ?? []).forEach((network, index) => {
    const subnets = results[index]?.data ?? [];
    byNetwork.set(network.id, subnets);
    subnets.forEach((subnet) => byId.set(subnet.id, subnet));
  });
  return { byNetwork, byId, loading: results.some((result) => result.isLoading) };
}

export function flavorMap(flavors: Flavor[] | undefined) {
  return new Map((flavors ?? []).map((flavor) => [flavor.id, flavor]));
}
