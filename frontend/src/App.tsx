import { Navigate, Route, Routes } from "react-router-dom";
import { lastProjectId, Layout, projectPath } from "./app/Layout";
import { useProjects } from "./app/queries";
import { useI18n } from "./i18n";
import { Cluster } from "./pages/admin/Cluster";
import { PublicImages } from "./pages/admin/PublicImages";
import { Dashboard } from "./pages/Dashboard";
import { Images } from "./pages/Images";
import { ConsoleWindow } from "./pages/VMConsole";
import { VMCreate } from "./pages/VMCreate";
import { VMDetail } from "./pages/VMDetail";
import { VMList } from "./pages/VMList";
import { VPCDetail } from "./pages/VPCDetail";
import { VPCList } from "./pages/VPCList";
import { Alert, Button, Empty, Spinner } from "./ui";

export function App() {
  return (
    <Routes>
      <Route path="/" element={<Home />} />
      {/* The console in its own window, outside the project layout. */}
      <Route path="/console/:projectId/:vmId" element={<ConsoleWindow />} />
      <Route path="/projects/:projectId" element={<Layout />}>
        <Route index element={<Dashboard />} />
        <Route path="vms" element={<VMList />} />
        <Route path="vms/new" element={<VMCreate />} />
        <Route path="vms/:vmId" element={<VMDetail />} />
        <Route path="vms/:vmId/:tab" element={<VMDetail />} />
        <Route path="images" element={<Images />} />
        <Route path="vpcs" element={<VPCList />} />
        <Route path="vpcs/:networkId" element={<VPCDetail />} />
        <Route path="*" element={<NotFound />} />
      </Route>
      <Route path="/admin" element={<Layout />}>
        <Route index element={<Navigate to="cluster" replace />} />
        <Route path="cluster" element={<Cluster />} />
        <Route path="images" element={<PublicImages />} />
        <Route path="*" element={<NotFound />} />
      </Route>
      <Route path="*" element={<Home />} />
    </Routes>
  );
}

// Home sends the visitor to the dashboard of the last project they used, or the first project.
function Home() {
  const { t, errorText } = useI18n();
  const projects = useProjects();
  if (projects.isLoading) {
    return <Empty title={<Spinner label={t.loading} />} />;
  }
  if (projects.isError) {
    return (
      <div style={{ padding: 32 }}>
        <Alert tone="error" action={<Button onClick={() => projects.refetch()}>{t.refresh}</Button>}>
          {errorText(projects.error)}
        </Alert>
      </div>
    );
  }
  const remembered = lastProjectId();
  const project = projects.data?.find((item) => item.id === remembered) ?? projects.data?.[0];
  if (!project) {
    return <Empty title={t.noProjects} />;
  }
  return <Navigate to={projectPath(project.id)} replace />;
}

function NotFound() {
  const { t } = useI18n();
  return <Empty title={t.notFound} />;
}
