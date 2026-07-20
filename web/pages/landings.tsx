import React from 'react';
import OrgResourceLanding from '../components/OrgResourceLanding';
import { api, unwrap } from '../services/api';

// Org-first landing pages: each resource section opens to the organizations the
// user belongs to; drilling into one shows that org's resources.

export const ProjectsLanding: React.FC = () => (
    <OrgResourceLanding title="Projects" basePath="/projects" unit="project" fetchItems={api.getProjects} />
);

export const InventoriesLanding: React.FC = () => (
    <OrgResourceLanding title="Inventories" basePath="/inventories" unit="inventory" pluralUnit="inventories" fetchItems={api.getInventories} />
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

// Schedules don't carry an org directly (derived from their target), so resolve
// each schedule's org via its template/workflow, and merge in event triggers
// (which do carry an org) for the per-org count.
const fetchScheduleItems = async () => {
    const [scheds, tpls, wfs, evts] = await Promise.all([
        api.getSchedules().catch(() => []),
        api.getTemplates().catch(() => ({})),
        api.getWorkflows().catch(() => []),
        api.getEventTriggers().catch(() => []),
    ]);
    const templates = unwrap<any>(tpls);
    const workflows = unwrap<any>(wfs);
    const ujtOrg = new Map<number, number>(templates.map((t: any) => [t.unified_job_template_id ?? t.id, t.organization_id]));
    const wfOrg = new Map<number, number>(workflows.map((w: any) => [w.id, w.organization_id]));
    const items: { organization_id: number }[] = [];
    for (const s of unwrap<any>(scheds)) {
        const org = s.workflow_template_id ? wfOrg.get(s.workflow_template_id) : ujtOrg.get(s.unified_job_template_id);
        if (org != null) items.push({ organization_id: org });
    }
    for (const e of unwrap<any>(evts)) if (e.organization_id != null) items.push({ organization_id: e.organization_id });
    return items;
};

export const SchedulesLanding: React.FC = () => (
    <OrgResourceLanding title="Schedules & Triggers" basePath="/schedules" unit="trigger" fetchItems={fetchScheduleItems} />
);
