import React, { useState } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import Layout from './components/Layout';
import LoginPage from './pages/LoginPage';
import DashboardPage from './pages/DashboardPage';
import JobsPage from './pages/JobsPage';
import JobDetailPage from './pages/JobDetailPage';
import TemplatesPage from './pages/TemplatesPage';
import WorkflowsPage from './pages/WorkflowsPage';
import WorkflowRunPage from './pages/WorkflowRunPage';
import ProjectsPage from './pages/ProjectsPage';
import InventoriesPage from './pages/InventoriesPage';
import CredentialsPage from './pages/CredentialsPage';
import ExecutionPacksPage from './pages/ExecutionPacksPage';
import SchedulesPage from './pages/SchedulesPage';
import UsersPage from './pages/UsersPage';
import TeamsPage from './pages/TeamsPage';
import ActivityPage from './pages/ActivityPage';
import OrganizationsPage from './pages/OrganizationsPage';
import AuthProvidersPage from './pages/AuthProvidersPage';
import { getAuthToken, removeAuthToken } from './services/api';

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
      <Routes>
        <Route
          path="/login"
          element={!isAuthenticated ? <LoginPage onLogin={handleLogin} /> : <Navigate to="/" replace />}
        />

        <Route
          path="/"
          element={isAuthenticated ? <Layout onLogout={handleLogout} /> : <Navigate to="/login" replace />}
        >
          <Route index element={<DashboardPage />} />
          <Route path="jobs" element={<JobsPage />} />
          <Route path="jobs/:jobId" element={<JobDetailPage />} />
          <Route path="templates" element={<TemplatesPage />} />
          <Route path="workflows" element={<WorkflowsPage />} />
          <Route path="workflows/runs/:jobId" element={<WorkflowRunPage />} />
          <Route path="projects" element={<ProjectsPage />} />
          <Route path="inventories" element={<InventoriesPage />} />
          <Route path="credentials" element={<CredentialsPage />} />
          <Route path="execution-packs" element={<ExecutionPacksPage />} />
          <Route path="schedules" element={<SchedulesPage />} />

          {/* RBAC Routes */}
          <Route path="organizations" element={<OrganizationsPage />} />
          <Route path="users" element={<UsersPage />} />
          <Route path="teams" element={<TeamsPage />} />
          <Route path="activity" element={<ActivityPage />} />
          <Route path="auth-providers" element={<AuthProvidersPage />} />

          {/* Fallback */}
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
};

export default App;