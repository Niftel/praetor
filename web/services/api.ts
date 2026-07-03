const API_BASE = '/api/v1';

export const getAuthToken = () => localStorage.getItem('praetor_token');
export const setAuthToken = (token: string) => localStorage.setItem('praetor_token', token);
export const removeAuthToken = () => localStorage.removeItem('praetor_token');

export const fetchWithAuth = async (endpoint: string, options: RequestInit = {}) => {
    const token = getAuthToken();
    const headers = new Headers(options.headers || {});

    if (token) {
        headers.set('Authorization', `Bearer ${token}`);
    }

    headers.set('Content-Type', 'application/json');

    const response = await fetch(`${API_BASE}${endpoint}`, {
        ...options,
        headers,
    });

    if (response.status === 401) {
        removeAuthToken();
        window.location.href = '/login';
        throw new Error('Unauthorized');
    }

    if (!response.ok) {
        const contentType = response.headers.get("content-type");
        if (contentType && contentType.indexOf("application/json") !== -1) {
            const errorData = await response.json();
            throw new Error(errorData.error || errorData.message || 'API request failed');
        }
        throw new Error(response.statusText || 'API request failed');
    }

    return response;
};

export const api = {
    // Auth
    login: async (credentials: any) => {
        const res = await fetch(`${API_BASE}/auth/login`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(credentials)
        });
        if (!res.ok) throw new Error('Login failed');
        return res.json();
    },

    // Jobs
    getJobs: () => fetchWithAuth('/jobs').then(r => r.json()),
    launchJob: (data: any) => fetchWithAuth('/jobs', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    cancelJob: (id: number) => fetchWithAuth(`/jobs/${id}/cancel`, { method: 'POST' }).then(r => r.json()),

    // API tokens (personal access tokens for headless/CI auth)
    listTokens: () => fetchWithAuth('/tokens').then(r => r.json()),
    createToken: (data: { name: string; expires_at?: string | null }) =>
        fetchWithAuth('/tokens', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    revokeToken: (id: number) => fetchWithAuth(`/tokens/${id}`, { method: 'DELETE' }).then(r => r.json()),

    // Dashboard Stats (derived from jobs for now)
    getDashboardStats: async () => {
        const jobs = await fetchWithAuth('/jobs').then(r => r.json());
        // Calculate stats on the fly or fetch from a dedicated endpoint if you have one
        return jobs;
    },

    // Templates
    getTemplates: () => fetchWithAuth('/job-templates').then(r => r.json()),
    createTemplate: (data: any) => fetchWithAuth('/job-templates', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    updateTemplate: (id: number, data: any) => fetchWithAuth(`/job-templates/${id}`, { method: 'PUT', body: JSON.stringify(data) }).then(r => r.json()),
    deleteTemplate: (id: number) => fetchWithAuth(`/job-templates/${id}`, { method: 'DELETE' }),

    // Projects
    getProjects: () => fetchWithAuth('/projects').then(r => r.json()),
    createProject: (data: any) => fetchWithAuth('/projects', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    syncProject: (id: number) => fetchWithAuth(`/projects/${id}/sync`, { method: 'POST' }),

    // Activity stream (audit log)
    getActivityStream: (limit = 100) => fetchWithAuth(`/activity-stream?limit=${limit}`).then(r => r.json()),

    // Inventory sources (dynamic inventory)
    getInventorySources: (invId: number) => fetchWithAuth(`/inventories/${invId}/sources`).then(r => r.json()),
    createInventorySource: (invId: number, data: any) => fetchWithAuth(`/inventories/${invId}/sources`, { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    deleteInventorySource: (invId: number, sid: number) => fetchWithAuth(`/inventories/${invId}/sources/${sid}`, { method: 'DELETE' }),
    syncInventorySource: (invId: number, sid: number) => fetchWithAuth(`/inventories/${invId}/sources/${sid}/sync`, { method: 'POST' }).then(r => r.json()),

    // Notifications
    getNotificationTemplates: (orgId: number) => fetchWithAuth(`/notification-templates?organization_id=${orgId}`).then(r => r.json()),
    createNotificationTemplate: (data: any) => fetchWithAuth('/notification-templates', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    deleteNotificationTemplate: (id: number) => fetchWithAuth(`/notification-templates/${id}`, { method: 'DELETE' }),
    getTemplateNotifications: (jtId: number) => fetchWithAuth(`/job-templates/${jtId}/notifications`).then(r => r.json()),
    attachTemplateNotification: (jtId: number, data: any) => fetchWithAuth(`/job-templates/${jtId}/notifications`, { method: 'POST', body: JSON.stringify(data) }),
    detachTemplateNotification: (jtId: number, ntId: number, event: string) => fetchWithAuth(`/job-templates/${jtId}/notifications/${ntId}/${event}`, { method: 'DELETE' }),

    // Workflows (DAG of job-template / approval nodes with success/failure/always edges)
    getWorkflows: () => fetchWithAuth('/workflow-templates').then(r => r.json()),
    getWorkflow: (id: number) => fetchWithAuth(`/workflow-templates/${id}`).then(r => r.json()),
    createWorkflow: (data: any) => fetchWithAuth('/workflow-templates', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    updateWorkflow: (id: number, data: any) => fetchWithAuth(`/workflow-templates/${id}`, { method: 'PUT', body: JSON.stringify(data) }).then(r => r.json()),
    deleteWorkflow: (id: number) => fetchWithAuth(`/workflow-templates/${id}`, { method: 'DELETE' }),
    launchWorkflow: (id: number) => fetchWithAuth(`/workflow-templates/${id}/launch`, { method: 'POST', body: '{}' }).then(r => r.json()),
    getWorkflowJobs: () => fetchWithAuth('/workflow-jobs').then(r => r.json()),
    getWorkflowJob: (id: number) => fetchWithAuth(`/workflow-jobs/${id}`).then(r => r.json()),
    approveWorkflowNode: (nodeId: number) => fetchWithAuth(`/workflow-job-nodes/${nodeId}/approve`, { method: 'POST' }),
    denyWorkflowNode: (nodeId: number) => fetchWithAuth(`/workflow-job-nodes/${nodeId}/deny`, { method: 'POST' }),
    // Triggers: event triggers (job outcome -> launch) + inbound webhook surface.
    getEventTriggers: () => fetchWithAuth('/triggers/event').then(r => r.json()),
    createEventTrigger: (data: any) => fetchWithAuth('/triggers/event', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    updateEventTrigger: (id: number, data: any) => fetchWithAuth(`/triggers/event/${id}`, { method: 'PUT', body: JSON.stringify(data) }).then(r => r.json()),
    deleteEventTrigger: (id: number) => fetchWithAuth(`/triggers/event/${id}`, { method: 'DELETE' }),
    getWebhookTriggers: () => fetchWithAuth('/triggers/webhook').then(r => r.json()),
    // Release a waiting webhook_in node via its (public, token-bearing) callback URL.
    releaseWorkflowNode: (callbackUrl: string, fail?: boolean) =>
      fetch(`${callbackUrl}${fail ? '&result=failed' : ''}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: fail ? 'failed' : 'successful' }),
      }).then(r => { if (!r.ok) throw new Error('callback failed'); return r; }),

    // Logs
    getJobEvents: (runId: string) => fetchWithAuth(`/jobs/runs/${runId}/events?limit=1000`).then(r => r.json()),
    // Full playbook stdout, reassembled from the object store (returns plain text).
    getJobLogs: (runId: string) => fetchWithAuth(`/jobs/runs/${runId}/logs`).then(r => r.text()),
    // Incremental tail: returns only chunks newer than `since` plus the new tail
    // cursor (X-Praetor-Last-Seq). Poll with the returned lastSeq to stream output
    // as it lands, appending rather than refetching the whole log. since=-1 = all.
    getJobLogsSince: async (runId: string, since: number) => {
        const r = await fetchWithAuth(`/jobs/runs/${runId}/logs?since=${since}`);
        const text = await r.text();
        const hdr = r.headers.get('X-Praetor-Last-Seq');
        return { text, lastSeq: hdr !== null && hdr !== '' ? Number(hdr) : since };
    },


    // Inventories
    getInventories: () => fetchWithAuth('/inventories').then(r => r.json()),
    getInventory: (id: number) => fetchWithAuth(`/inventories/${id}`).then(r => r.json()),
    createInventory: (data: any) => fetchWithAuth('/inventories', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    updateInventory: (id: number, data: any) => fetchWithAuth(`/inventories/${id}`, { method: 'PUT', body: JSON.stringify(data) }).then(r => r.json()),
    deleteInventory: (id: number) => fetchWithAuth(`/inventories/${id}`, { method: 'DELETE' }),
    importInventory: (inventoryId: number, content: string, format: 'ini' | 'yaml') =>
        fetchWithAuth(`/inventories/${inventoryId}/import`, {
            method: 'POST',
            body: JSON.stringify({ content, format })
        }).then(r => r.json()),

    // Hosts (nested under inventories)
    getHosts: (inventoryId: number) => fetchWithAuth(`/inventories/${inventoryId}/hosts`).then(r => r.json()),
    getHost: (hostId: number) => fetchWithAuth(`/hosts/${hostId}`).then(r => r.json()),
    createHost: (inventoryId: number, data: any) => fetchWithAuth(`/inventories/${inventoryId}/hosts`, { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    updateHost: (hostId: number, data: any) => fetchWithAuth(`/hosts/${hostId}`, { method: 'PUT', body: JSON.stringify(data) }).then(r => r.json()),
    deleteHost: (hostId: number) => fetchWithAuth(`/hosts/${hostId}`, { method: 'DELETE' }),
    setRunnerHost: (hostId: number) => fetchWithAuth(`/hosts/${hostId}/set-runner`, { method: 'POST' }).then(r => r.json()),

    // Groups (nested under inventories)
    getGroups: (inventoryId: number) => fetchWithAuth(`/inventories/${inventoryId}/groups`).then(r => r.json()),
    getGroup: (groupId: number) => fetchWithAuth(`/groups/${groupId}`).then(r => r.json()),
    createGroup: (inventoryId: number, data: any) => fetchWithAuth(`/inventories/${inventoryId}/groups`, { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    updateGroup: (groupId: number, data: any) => fetchWithAuth(`/groups/${groupId}`, { method: 'PUT', body: JSON.stringify(data) }).then(r => r.json()),
    deleteGroup: (groupId: number) => fetchWithAuth(`/groups/${groupId}`, { method: 'DELETE' }),
    getGroupHosts: (groupId: number) => fetchWithAuth(`/groups/${groupId}/hosts`).then(r => r.json()),
    addHostToGroup: (groupId: number, hostId: number) => fetchWithAuth(`/groups/${groupId}/hosts`, { method: 'POST', body: JSON.stringify({ host_id: hostId }) }),
    removeHostFromGroup: (groupId: number, hostId: number) => fetchWithAuth(`/groups/${groupId}/hosts/${hostId}`, { method: 'DELETE' }),
    getHostGroups: (hostId: number) => fetchWithAuth(`/hosts/${hostId}/groups`).then(r => r.json()),

    // Credentials
    getCredentials: () => fetchWithAuth('/credentials').then(r => r.json()),
    getCredential: (id: number) => fetchWithAuth(`/credentials/${id}`).then(r => r.json()),
    createCredential: (data: any) => fetchWithAuth('/credentials', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    updateCredential: (id: number, data: any) => fetchWithAuth(`/credentials/${id}`, { method: 'PUT', body: JSON.stringify(data) }).then(r => r.json()),
    deleteCredential: (id: number) => fetchWithAuth(`/credentials/${id}`, { method: 'DELETE' }),
    getCredentialTypes: () => fetchWithAuth('/credential-types').then(r => r.json()),

    // Execution Packs — the self-contained runtimes pushed to hosts.
    getExecutionPacks: () => fetchWithAuth('/execution-packs').then(r => r.json()),
    createExecutionPack: (data: any) => fetchWithAuth('/execution-packs', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    updateExecutionPack: (id: number, data: any) => fetchWithAuth(`/execution-packs/${id}`, { method: 'PUT', body: JSON.stringify(data) }).then(r => r.json()),
    rebuildExecutionPack: (id: number) => fetchWithAuth(`/execution-packs/${id}/rebuild`, { method: 'POST' }),
    deleteExecutionPack: (id: number) => fetchWithAuth(`/execution-packs/${id}`, { method: 'DELETE' }),

    // Schedules
    getSchedules: () => fetchWithAuth('/schedules').then(r => r.json()),
    getSchedule: (id: number) => fetchWithAuth(`/schedules/${id}`).then(r => r.json()),
    createSchedule: (data: any) => fetchWithAuth('/schedules', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    updateSchedule: (id: number, data: any) => fetchWithAuth(`/schedules/${id}`, { method: 'PUT', body: JSON.stringify(data) }).then(r => r.json()),
    deleteSchedule: (id: number) => fetchWithAuth(`/schedules/${id}`, { method: 'DELETE' }),

    // Users
    getUsers: () => fetchWithAuth('/users').then(r => r.json()),
    getUser: (id: number) => fetchWithAuth(`/users/${id}`).then(r => r.json()),
    createUser: (data: any) => fetchWithAuth('/users', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    updateUser: (id: number, data: any) => fetchWithAuth(`/users/${id}`, { method: 'PUT', body: JSON.stringify(data) }).then(r => r.json()),
    deleteUser: (id: number) => fetchWithAuth(`/users/${id}`, { method: 'DELETE' }),

    // Teams
    getTeams: () => fetchWithAuth('/teams').then(r => r.json()),
    getTeam: (id: number) => fetchWithAuth(`/teams/${id}`).then(r => r.json()),
    createTeam: (data: any) => fetchWithAuth('/teams', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    updateTeam: (id: number, data: any) => fetchWithAuth(`/teams/${id}`, { method: 'PUT', body: JSON.stringify(data) }).then(r => r.json()),
    deleteTeam: (id: number) => fetchWithAuth(`/teams/${id}`, { method: 'DELETE' }),
    getTeamMembers: (teamId: number) => fetchWithAuth(`/teams/${teamId}/members`).then(r => r.json()),
    addTeamMember: (teamId: number, userId: number) => fetchWithAuth(`/teams/${teamId}/members`, { method: 'POST', body: JSON.stringify({ user_id: userId }) }),
    removeTeamMember: (teamId: number, userId: number) => fetchWithAuth(`/teams/${teamId}/members/${userId}`, { method: 'DELETE' }),

    // Roles (AWX-style)
    getRoles: () => fetchWithAuth('/roles').then(r => r.json()),
    getRole: (id: number) => fetchWithAuth(`/roles/${id}`).then(r => r.json()),
    // AWX-style access: roles on a resource, and roles a user holds.
    getResourceAccess: (contentType: string, objectId: number) => fetchWithAuth(`/access?content_type=${contentType}&object_id=${objectId}`).then(r => r.json()),
    getUserAccess: (userId: number) => fetchWithAuth(`/users/${userId}/access`).then(r => r.json()),
    getRoleUsers: (roleId: number) => fetchWithAuth(`/roles/${roleId}/users`).then(r => r.json()),
    addRoleUser: (roleId: number, userId: number) => fetchWithAuth(`/roles/${roleId}/users`, { method: 'POST', body: JSON.stringify({ user_id: userId }) }),
    removeRoleUser: (roleId: number, userId: number) => fetchWithAuth(`/roles/${roleId}/users/${userId}`, { method: 'DELETE' }),
    getRoleTeams: (roleId: number) => fetchWithAuth(`/roles/${roleId}/teams`).then(r => r.json()),
    addRoleTeam: (roleId: number, teamId: number) => fetchWithAuth(`/roles/${roleId}/teams`, { method: 'POST', body: JSON.stringify({ team_id: teamId }) }),
    removeRoleTeam: (roleId: number, teamId: number) => fetchWithAuth(`/roles/${roleId}/teams/${teamId}`, { method: 'DELETE' }),

    // Organizations
    getOrganizations: () => fetchWithAuth('/organizations').then(r => r.json()),
    getOrganization: (id: number) => fetchWithAuth(`/organizations/${id}`).then(r => r.json()),
    createOrganization: (data: any) => fetchWithAuth('/organizations', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    updateOrganization: (id: number, data: any) => fetchWithAuth(`/organizations/${id}`, { method: 'PUT', body: JSON.stringify(data) }).then(r => r.json()),
    deleteOrganization: (id: number) => fetchWithAuth(`/organizations/${id}`, { method: 'DELETE' }),
    getOrganizationUsers: (orgId: number) => fetchWithAuth(`/organizations/${orgId}/users`).then(r => r.json()),
    addOrganizationUser: (orgId: number, userId: number) => fetchWithAuth(`/organizations/${orgId}/users`, { method: 'POST', body: JSON.stringify({ user_id: userId }) }),
    removeOrganizationUser: (orgId: number, userId: number) => fetchWithAuth(`/organizations/${orgId}/users/${userId}`, { method: 'DELETE' }),
    getOrganizationAdmins: (orgId: number) => fetchWithAuth(`/organizations/${orgId}/admins`).then(r => r.json()),
    addOrganizationAdmin: (orgId: number, userId: number) => fetchWithAuth(`/organizations/${orgId}/admins`, { method: 'POST', body: JSON.stringify({ user_id: userId }) }),
    getOrganizationTeams: (orgId: number) => fetchWithAuth(`/organizations/${orgId}/teams`).then(r => r.json()),
    getOrganizationRoles: (orgId: number) => fetchWithAuth(`/organizations/${orgId}/object_roles`).then(r => r.json()),
    getOrgGalaxyCredentials: (orgId: number) => fetchWithAuth(`/organizations/${orgId}/galaxy-credentials`).then(r => r.json()),
    addOrgGalaxyCredential: (orgId: number, credentialId: number) => fetchWithAuth(`/organizations/${orgId}/galaxy-credentials`, { method: 'POST', body: JSON.stringify({ credential_id: credentialId }) }),
    removeOrgGalaxyCredential: (orgId: number, credId: number) => fetchWithAuth(`/organizations/${orgId}/galaxy-credentials/${credId}`, { method: 'DELETE' }),

    // User relationships
    getUserOrganizations: (userId: number) => fetchWithAuth(`/users/${userId}/organizations`).then(r => r.json()),
    getUserTeams: (userId: number) => fetchWithAuth(`/users/${userId}/teams`).then(r => r.json()),
    getUserRoles: (userId: number) => fetchWithAuth(`/users/${userId}/roles`).then(r => r.json()),

    // Legacy role bindings (kept for backwards compat)
    getRoleBindings: () => fetchWithAuth('/role_bindings').then(r => r.json()),
    createRoleBinding: (data: any) => fetchWithAuth('/role_bindings', { method: 'POST', body: JSON.stringify(data) }).then(r => r.json()),
    deleteRoleBinding: (id: number) => fetchWithAuth(`/role_bindings/${id}`, { method: 'DELETE' }),

    // LDAP Configuration
    getLdapConfig: () => fetchWithAuth('/ldap/config').then(r => r.json()),
    testLdapConnection: () => fetchWithAuth('/ldap/test-connection', { method: 'POST' }).then(r => r.json()),
    triggerLdapSync: () => fetchWithAuth('/ldap/sync', { method: 'POST' }).then(r => r.json()),
    getLdapSyncStatus: () => fetchWithAuth('/ldap/sync/status').then(r => r.json()),
    getLdapSyncDetails: (id: number) => fetchWithAuth(`/ldap/sync/${id}`).then(r => r.json()),
};


