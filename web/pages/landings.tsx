import React from 'react';
import OrgResourceLanding from '../components/OrgResourceLanding';
import { api } from '../services/api';

// Org-first landing pages: each resource section opens to the organizations the
// user belongs to; drilling into one shows that org's resources.

export const ProjectsLanding: React.FC = () => (
    <OrgResourceLanding title="Projects" basePath="/projects" unit="project" fetchItems={api.getProjects} />
);

export const InventoriesLanding: React.FC = () => (
    <OrgResourceLanding title="Inventories" basePath="/inventories" unit="inventory" fetchItems={api.getInventories} />
);

export const TemplatesLanding: React.FC = () => (
    <OrgResourceLanding title="Templates" basePath="/templates" unit="template" fetchItems={api.getTemplates} />
);

export const WorkflowsLanding: React.FC = () => (
    <OrgResourceLanding title="Workflows" basePath="/workflows" unit="workflow" fetchItems={api.getWorkflows} />
);

export const CredentialsLanding: React.FC = () => (
    <OrgResourceLanding title="Credentials" basePath="/credentials" unit="credential" fetchItems={api.getCredentials} />
);
