import { useQuery } from "@tanstack/react-query";
import { Activity, Gauge, Image as ImageIcon, LayoutDashboard, Network, Server, ShieldCheck } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { createContext, useContext, useEffect, useState } from "react";
import { NavLink, Outlet, useLocation, useNavigate, useParams } from "react-router-dom";
import { api } from "../api";
import { languageOptions, useI18n } from "../i18n";
import type { Language } from "../i18n";
import { Badge, Select, Switch } from "../ui";
import { keys, useProjects } from "./queries";

const LAST_PROJECT_KEY = "kloudia-project";
const ADMIN_MODE_KEY = "kloudia-admin-mode";

function readStorage(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function writeStorage(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // Preference not persisted; it still applies to this page.
  }
}

export function lastProjectId(): string {
  return readStorage(LAST_PROJECT_KEY) ?? "";
}

type Shell = { projectId: string; projectName: string; adminMode: boolean };
const ShellContext = createContext<Shell>({ projectId: "", projectName: "", adminMode: false });

// useProject returns the project the console is working in: the one in the URL, or the last one used.
export function useProject() {
  return useContext(ShellContext);
}

export function projectPath(projectId: string, path = "") {
  return `/projects/${projectId}${path ? `/${path}` : ""}`;
}

export function Layout() {
  const { t, language, setLanguage } = useI18n();
  const params = useParams();
  const location = useLocation();
  const navigate = useNavigate();
  const projects = useProjects();
  const [adminMode, setAdminMode] = useState(() => readStorage(ADMIN_MODE_KEY) === "true");
  const health = useQuery({ queryKey: keys.health, queryFn: api.health, refetchInterval: 30000, retry: 0 });

  const urlProjectId = params.projectId ?? "";
  const remembered = lastProjectId();
  const fallbackProjectId =
    projects.data?.find((project) => project.id === remembered)?.id ?? projects.data?.[0]?.id ?? "";
  const projectId = urlProjectId || fallbackProjectId;
  const projectName = projects.data?.find((project) => project.id === projectId)?.name ?? projectId;

  useEffect(() => {
    if (urlProjectId) {
      writeStorage(LAST_PROJECT_KEY, urlProjectId);
    }
  }, [urlProjectId]);

  const isAdminPage = location.pathname.startsWith("/admin");

  function changeAdminMode(value: boolean) {
    setAdminMode(value);
    writeStorage(ADMIN_MODE_KEY, String(value));
    if (!value && isAdminPage && projectId) {
      navigate(projectPath(projectId));
    }
  }

  function changeProject(nextProjectId: string) {
    // Stay in the same section (VMs, VPCs, …) of the new project; a VM or VPC of the old one does not exist there.
    const section = urlProjectId ? location.pathname.split("/")[3] ?? "" : "";
    navigate(projectPath(nextProjectId, section === "dashboard" ? "" : section));
  }

  const healthStatus = health.data?.status ?? (health.isError ? "offline" : "");

  return (
    <ShellContext.Provider value={{ projectId, projectName, adminMode }}>
      <div className="shell">
        <header className="topbar">
          <div className="topbar-left">
            <NavLink to={projectId ? projectPath(projectId) : "/"} className="brand">
              <span className="brand-mark" aria-hidden>
                K
              </span>
              <span className="brand-name">{t.appName}</span>
            </NavLink>
            <label className="project-picker">
              <span className="topbar-hint">{t.project}</span>
              <Select
                aria-label={t.projectSelect}
                value={projectId}
                onChange={(event) => changeProject(event.target.value)}
                disabled={!projects.data || projects.data.length === 0}
              >
                {(projects.data ?? []).map((project) => (
                  <option key={project.id} value={project.id}>
                    {project.name}
                  </option>
                ))}
              </Select>
            </label>
          </div>
          <div className="topbar-right">
            {healthStatus && (
              <Badge
                tone={healthStatus === "ok" ? "success" : healthStatus === "degraded" ? "warning" : "error"}
                dot
                title={health.data?.version ? `kloudia-api ${health.data.version}` : undefined}
              >
                <Activity size={12} aria-hidden />
                {healthStatus === "ok" ? t.apiOk : healthStatus === "degraded" ? t.apiDegraded : t.apiOffline}
              </Badge>
            )}
            <Switch checked={adminMode} onChange={changeAdminMode} label={t.adminMode} />
            <Select
              aria-label={t.language}
              className="input select language-select"
              value={language}
              onChange={(event) => setLanguage(event.target.value as Language)}
            >
              {languageOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </Select>
          </div>
        </header>
        <div className="shell-body">
          <nav className="sidebar" aria-label={t.navMain}>
            {projectId && (
              <>
                <NavItem to={projectPath(projectId)} icon={LayoutDashboard} end>
                  {t.navDashboard}
                </NavItem>
                <span className="nav-section">{t.navCompute}</span>
                <NavItem to={projectPath(projectId, "vms")} icon={Server}>
                  {t.navVMs}
                </NavItem>
                <NavItem to={projectPath(projectId, "images")} icon={ImageIcon}>
                  {t.navImages}
                </NavItem>
                <span className="nav-section">{t.navNetwork}</span>
                <NavItem to={projectPath(projectId, "vpcs")} icon={Network}>
                  {t.navVPCs}
                </NavItem>
              </>
            )}
            {adminMode && (
              <>
                <span className="nav-section nav-section-admin">
                  <ShieldCheck size={12} aria-hidden /> {t.navAdmin}
                </span>
                <NavItem to="/admin/cluster" icon={Gauge}>
                  {t.navCluster}
                </NavItem>
                <NavItem to="/admin/images" icon={ImageIcon}>
                  {t.navPublicImages}
                </NavItem>
              </>
            )}
          </nav>
          <main className="content">
            <Outlet />
          </main>
        </div>
      </div>
    </ShellContext.Provider>
  );
}

function NavItem({ to, icon: Icon, children, end }: { to: string; icon: LucideIcon; children: ReactNode; end?: boolean }) {
  return (
    <NavLink to={to} end={end} className={({ isActive }) => (isActive ? "nav-item nav-item-active" : "nav-item")}>
      <Icon size={18} aria-hidden />
      <span>{children}</span>
    </NavLink>
  );
}
