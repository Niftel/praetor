import React, { lazy, Suspense, useState } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import Shell from './components/Shell';
import LoginPage from './pages/LoginPage';
import { ToastHost } from './components/ui/toast';
import { LoadingState } from './components/ui';
import { getAuthToken, removeAuthToken } from './services/api';

const DashboardPage = lazy(() => import('./pages/DashboardPage'));
const JobsPage = lazy(() => import('./pages/JobsPage'));
const JobDetailPage = lazy(() => import('./pages/JobDetailPage'));
const TemplatesPage = lazy(() => import('./pages/TemplatesPage'));
const WorkflowsPage = lazy(() => import('./pages/WorkflowsPage'));
const WorkflowBuilderPage = lazy(() => import('./pages/WorkflowBuilderPage'));
const WorkflowRunPage = lazy(() => import('./pages/WorkflowRunPage'));
const ProjectsPage = lazy(() => import('./pages/ProjectsPage'));
const InventoriesPage = lazy(() => import('./pages/InventoriesPage'));
const CredentialsPage = lazy(() => import('./pages/CredentialsPage'));
const ExecutionPacksPage = lazy(() => import('./pages/ExecutionPacksPage'));
const TokensPage = lazy(() => import('./pages/TokensPage'));
const ServicePrincipalsPage = lazy(() => import('./pages/ServicePrincipalsPage'));
const SchedulesPage = lazy(() => import('./pages/SchedulesPage'));
const UsersPage = lazy(() => import('./pages/UsersPage'));
const TeamsPage = lazy(() => import('./pages/TeamsPage'));
const ActivityPage = lazy(() => import('./pages/ActivityPage'));
const OrganizationsPage = lazy(() => import('./pages/OrganizationsPage'));
const AuthProvidersPage = lazy(() => import('./pages/AuthProvidersPage'));
const SettingsPage = lazy(() => import('./pages/SettingsPage'));
const ApprovalsPage = lazy(() => import('./pages/ApprovalsPage'));
const WorkflowDagPreview = lazy(() => import('./pages/WorkflowDagPreview'));
const ProjectsLanding = lazy(() => import('./pages/landings').then(module => ({ default: module.ProjectsLanding })));
const InventoriesLanding = lazy(() => import('./pages/landings').then(module => ({ default: module.InventoriesLanding })));
const TemplatesLanding = lazy(() => import('./pages/landings').then(module => ({ default: module.TemplatesLanding })));
const WorkflowsLanding = lazy(() => import('./pages/landings').then(module => ({ default: module.WorkflowsLanding })));
const CredentialsLanding = lazy(() => import('./pages/landings').then(module => ({ default: module.CredentialsLanding })));
const SchedulesLanding = lazy(() => import('./pages/landings').then(module => ({ default: module.SchedulesLanding })));

const App = () => {
  // Check if we have an existing token on initialization
  const [isAuthenticated, setIsAuthenticated] = useState(() => !!getAuthToken());

  const handleLogin = () => setIsAuthenticated(true);
  const handleLogout = () => {
    removeAuthToken();
    setIsAuthenticated(false);
  };

  return (
    <BrowserRouter>
      <ToastHost />
      <Routes>
        <Route
          path="/login"
          element={!isAuthenticated ? <LoginPage onLogin={handleLogin} /> : <Navigate to="/" replace />}
        />

        {/* Dev-only visual check for WorkflowDag — no auth, no backend. Stripped
            from production builds via import.meta.env.DEV. */}
        {import.meta.env.DEV && (
          <Route
            path="/_preview/workflow-dag"
            element={(
              <Suspense fallback={<LoadingState label="Loading preview" />}>
                <WorkflowDagPreview />
              </Suspense>
            )}
          />
        )}

        <Route
          path="/"
          element={isAuthenticated ? <Shell onLogout={handleLogout} /> : <Navigate to="/login" replace />}
        >
          <Route index element={<DashboardPage />} />
          <Route path="jobs" element={<JobsPage />} />
          <Route path="jobs/:jobId" element={<JobDetailPage />} />
          {/* Org-first: each resource opens to the user's orgs, then drills in. */}
          <Route path="templates" element={<TemplatesLanding />} />
          <Route path="templates/org/:orgId" element={<TemplatesPage />} />
          <Route path="workflows" element={<WorkflowsLanding />} />
          <Route path="workflows/org/:orgId" element={<WorkflowsPage />} />
          <Route path="workflows/org/:orgId/builder" element={<WorkflowBuilderPage />} />
          <Route path="workflows/org/:orgId/builder/:workflowId" element={<WorkflowBuilderPage />} />
          <Route path="workflows/runs/:jobId" element={<WorkflowRunPage />} />
          <Route path="approvals" element={<ApprovalsPage />} />
          <Route path="projects" element={<ProjectsLanding />} />
          <Route path="projects/org/:orgId" element={<ProjectsPage />} />
          <Route path="inventories" element={<InventoriesLanding />} />
          <Route path="inventories/org/:orgId" element={<InventoriesPage />} />
          <Route path="credentials" element={<CredentialsLanding />} />
          <Route path="credentials/org/:orgId" element={<CredentialsPage />} />
          <Route path="tokens" element={<TokensPage />} />
          <Route path="service-principals" element={<ServicePrincipalsPage />} />
          <Route path="execution-packs" element={<ExecutionPacksPage />} />
          <Route path="schedules" element={<SchedulesLanding />} />
          <Route path="schedules/org/:orgId" element={<SchedulesPage />} />

          {/* RBAC Routes */}
          <Route path="organizations" element={<OrganizationsPage />} />
          <Route path="users" element={<UsersPage />} />
          <Route path="teams" element={<TeamsPage />} />
          <Route path="activity" element={<ActivityPage />} />

          {/* Settings */}
          <Route path="settings" element={<SettingsPage />} />
          <Route path="settings/auth-providers" element={<AuthProvidersPage />} />
          {/* Back-compat: the old top-level path now lives under Settings */}
          <Route path="auth-providers" element={<Navigate to="/settings/auth-providers" replace />} />

          {/* Fallback */}
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
};

export default App;
