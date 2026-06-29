import React, { useState } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import Layout from './components/Layout';
import LoginPage from './pages/LoginPage';
import DashboardPage from './pages/DashboardPage';
import JobsPage from './pages/JobsPage';
import TemplatesPage from './pages/TemplatesPage';
import ProjectsPage from './pages/ProjectsPage';
import InventoriesPage from './pages/InventoriesPage';
import CredentialsPage from './pages/CredentialsPage';
import SchedulesPage from './pages/SchedulesPage';
import UsersPage from './pages/UsersPage';
import TeamsPage from './pages/TeamsPage';
import RolesPage from './pages/RolesPage';
import OrganizationsPage from './pages/OrganizationsPage';
import InstancesPage from './pages/InstancesPage';
import InstanceGroupsPage from './pages/InstanceGroupsPage';
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
          <Route path="instances" element={<InstancesPage />} />
          <Route path="instance-groups" element={<InstanceGroupsPage />} />
          <Route path="jobs" element={<JobsPage />} />
          <Route path="templates" element={<TemplatesPage />} />
          <Route path="projects" element={<ProjectsPage />} />
          <Route path="inventories" element={<InventoriesPage />} />
          <Route path="credentials" element={<CredentialsPage />} />
          <Route path="schedules" element={<SchedulesPage />} />

          {/* RBAC Routes */}
          <Route path="organizations" element={<OrganizationsPage />} />
          <Route path="users" element={<UsersPage />} />
          <Route path="teams" element={<TeamsPage />} />
          <Route path="roles" element={<RolesPage />} />
          <Route path="auth-providers" element={<AuthProvidersPage />} />

          {/* Fallback */}
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
};

export default App;