import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Settings, RefreshCw, CheckCircle, XCircle, AlertCircle, Clock, Plus, Trash2, Edit2, Key, Users, Building, Github } from 'lucide-react';

interface AuthProvider {
    id: number;
    name: string;
    type: 'ldap' | 'saml' | 'github' | 'oauth2';
    enabled: boolean;
    config: any;
    created_at: string;
    modified_at: string;
}

interface LdapConfig {
    configured: boolean;
    config_path: string;
    config_error?: string;
    server?: {
        url: string;
        bind_dn: string;
        start_tls: boolean;
        timeout: string;
    };
    users?: {
        search_base: string;
        search_filter: string;
        search_scope: string;
    };
    organizations?: {
        enabled: boolean;
        search_base: string;
        search_filter: string;
    };
    teams?: {
        enabled: boolean;
        search_base: string;
        search_filter: string;
    };
    sync?: {
        interval: string;
        create_users: boolean;
        create_orgs: boolean;
        create_teams: boolean;
        remove_stale: boolean;
        dry_run: boolean;
    };
}

interface SyncLogEntry {
    id: number;
    sync_type: string;
    started_at: string;
    finished_at?: string;
    status: string;
    items_processed: number;
    items_created: number;
    items_updated: number;
    items_failed: number;
    error_message?: string;
}

interface SyncItem {
    id: number;
    entity_type: string;
    entity_name: string;
    entity_id?: number;
    ldap_dn: string;
    ldap_attributes?: Record<string, string[]>;
    action: string;
    error_message?: string;
    created_at: string;
}

interface SyncDetails {
    log: SyncLogEntry;
    items: SyncItem[];
}

const AuthProvidersPage: React.FC = () => {
    const [activeTab, setActiveTab] = useState<'ldap' | 'saml' | 'github' | 'oauth2'>('ldap');
    const [ldapConfig, setLdapConfig] = useState<LdapConfig | null>(null);
    const [syncLogs, setSyncLogs] = useState<SyncLogEntry[]>([]);
    const [loading, setLoading] = useState(true);
    const [syncing, setSyncing] = useState(false);
    const [testing, setTesting] = useState(false);
    const [testResult, setTestResult] = useState<{ success: boolean; message?: string; error?: string } | null>(null);
    const [expandedLogId, setExpandedLogId] = useState<number | null>(null);
    const [expandedItems, setExpandedItems] = useState<SyncItem[]>([]);

    useEffect(() => {
        loadData();
    }, []);

    const loadData = async () => {
        setLoading(true);
        try {
            const [configData, syncData] = await Promise.all([
                api.getLdapConfig(),
                api.getLdapSyncStatus(),
            ]);
            setLdapConfig(configData);
            setSyncLogs(syncData.results || []);
        } catch (error) {
            console.error('Failed to load LDAP data:', error);
        } finally {
            setLoading(false);
        }
    };

    const toggleSyncDetails = async (id: number) => {
        if (expandedLogId === id) {
            // Collapse if clicking same row
            setExpandedLogId(null);
            setExpandedItems([]);
            return;
        }

        setExpandedLogId(id);
        try {
            const details = await api.getLdapSyncDetails(id);
            setExpandedItems(details.items || []);
        } catch (error) {
            console.error('Failed to load sync details:', error);
            setExpandedItems([]);
        }
    };

    const handleTestConnection = async () => {
        setTesting(true);
        setTestResult(null);
        try {
            const result = await api.testLdapConnection();
            setTestResult(result);
        } catch (error: any) {
            setTestResult({ success: false, error: error.message });
        } finally {
            setTesting(false);
        }
    };

    const handleSync = async () => {
        setSyncing(true);
        try {
            await api.triggerLdapSync();
            await loadData();
        } catch (error) {
            console.error('Sync failed:', error);
        } finally {
            setSyncing(false);
        }
    };

    const getStatusIcon = (status: string) => {
        switch (status) {
            case 'success':
                return <CheckCircle className="text-green-500" size={16} />;
            case 'failed':
                return <XCircle className="text-red-500" size={16} />;
            case 'partial':
                return <AlertCircle className="text-yellow-500" size={16} />;
            case 'running':
                return <Clock className="text-blue-500 animate-pulse" size={16} />;
            default:
                return <AlertCircle className="text-gray-500" size={16} />;
        }
    };

    const getProviderIcon = (type: string) => {
        switch (type) {
            case 'ldap':
                return <Users size={24} />;
            case 'saml':
                return <Key size={24} />;
            case 'github':
                return <Github size={24} />;
            case 'oauth2':
                return <Building size={24} />;
            default:
                return <Settings size={24} />;
        }
    };

    const tabs = [
        { id: 'ldap', name: 'LDAP', icon: <Users size={16} /> },
        { id: 'saml', name: 'SAML', icon: <Key size={16} /> },
        { id: 'github', name: 'GitHub', icon: <Github size={16} /> },
        { id: 'oauth2', name: 'OAuth2 / OIDC', icon: <Building size={16} /> },
    ];

    if (loading) {
        return (
            <div className="flex items-center justify-center h-64">
                <RefreshCw className="animate-spin text-brand-500" size={32} />
            </div>
        );
    }

    return (
        <div className="p-6 space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-2xl font-bold text-gray-900">Authentication Providers</h1>
                    <p className="text-gray-600 mt-1">Configure LDAP, SAML, GitHub, and OAuth2 authentication</p>
                </div>
            </div>

            {/* Provider Tabs */}
            <div className="border-b border-gray-200">
                <nav className="-mb-px flex space-x-8">
                    {tabs.map((tab) => (
                        <button
                            key={tab.id}
                            onClick={() => setActiveTab(tab.id as any)}
                            className={`flex items-center gap-2 py-4 px-1 border-b-2 font-medium text-sm transition-colors ${activeTab === tab.id
                                ? 'border-brand-500 text-brand-600'
                                : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
                                }`}
                        >
                            {tab.icon}
                            {tab.name}
                        </button>
                    ))}
                </nav>
            </div>

            {/* LDAP Tab */}
            {activeTab === 'ldap' && (
                <div className="space-y-6">
                    {/* LDAP Configuration Card */}
                    <div className="bg-white rounded-lg shadow-sm border border-gray-200 overflow-hidden">
                        <div className="px-6 py-4 border-b border-gray-200 bg-gray-50">
                            <div className="flex items-center justify-between">
                                <div className="flex items-center gap-3">
                                    <Users className="text-brand-600" size={24} />
                                    <h2 className="text-lg font-semibold text-gray-900">LDAP Configuration</h2>
                                </div>
                                <div className="flex items-center gap-2">
                                    <button
                                        onClick={handleTestConnection}
                                        disabled={!ldapConfig?.configured || testing}
                                        className="px-4 py-2 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-md hover:bg-gray-50 disabled:opacity-50 disabled:cursor-not-allowed"
                                    >
                                        {testing ? 'Testing...' : 'Test Connection'}
                                    </button>
                                    <button
                                        onClick={handleSync}
                                        disabled={!ldapConfig?.configured || syncing}
                                        className="px-4 py-2 text-sm font-medium text-white bg-brand-600 rounded-md hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
                                    >
                                        <RefreshCw size={16} className={syncing ? 'animate-spin' : ''} />
                                        {syncing ? 'Syncing...' : 'Sync Now'}
                                    </button>
                                </div>
                            </div>
                        </div>

                        <div className="p-6">
                            {testResult && (
                                <div className={`mb-4 p-4 rounded-md ${testResult.success ? 'bg-green-50 border border-green-200' : 'bg-red-50 border border-red-200'}`}>
                                    <div className="flex items-center gap-2">
                                        {testResult.success ? <CheckCircle className="text-green-500" size={20} /> : <XCircle className="text-red-500" size={20} />}
                                        <span className={testResult.success ? 'text-green-700' : 'text-red-700'}>
                                            {testResult.success ? testResult.message : testResult.error}
                                        </span>
                                    </div>
                                </div>
                            )}

                            {!ldapConfig?.configured ? (
                                <div className="text-center py-8">
                                    <AlertCircle className="mx-auto text-yellow-500 mb-4" size={48} />
                                    <h3 className="text-lg font-medium text-gray-900 mb-2">LDAP Not Configured</h3>
                                    <p className="text-gray-600 mb-4">
                                        Create a configuration file at <code className="bg-gray-100 px-2 py-1 rounded">{ldapConfig?.config_path}</code>
                                    </p>
                                    <p className="text-sm text-gray-500">
                                        See <code className="bg-gray-100 px-1 rounded">playbooks/ldap-config.example.yaml</code> for an example.
                                    </p>
                                </div>
                            ) : ldapConfig.config_error ? (
                                <div className="bg-red-50 border border-red-200 rounded-md p-4">
                                    <div className="flex items-start gap-3">
                                        <XCircle className="text-red-500 mt-0.5" size={20} />
                                        <div>
                                            <h4 className="font-medium text-red-800">Configuration Error</h4>
                                            <pre className="mt-2 text-sm text-red-700 whitespace-pre-wrap">{ldapConfig.config_error}</pre>
                                        </div>
                                    </div>
                                </div>
                            ) : (
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                                    <div>
                                        <h4 className="font-medium text-gray-900 mb-3">Server</h4>
                                        <dl className="space-y-2 text-sm">
                                            <div className="flex justify-between">
                                                <dt className="text-gray-500">URL</dt>
                                                <dd className="text-gray-900 font-mono">{ldapConfig.server?.url}</dd>
                                            </div>
                                            <div className="flex justify-between">
                                                <dt className="text-gray-500">Bind DN</dt>
                                                <dd className="text-gray-900 font-mono text-xs">{ldapConfig.server?.bind_dn}</dd>
                                            </div>
                                            <div className="flex justify-between">
                                                <dt className="text-gray-500">StartTLS</dt>
                                                <dd className="text-gray-900">{ldapConfig.server?.start_tls ? 'Yes' : 'No'}</dd>
                                            </div>
                                        </dl>
                                    </div>
                                    <div>
                                        <h4 className="font-medium text-gray-900 mb-3">Users</h4>
                                        <dl className="space-y-2 text-sm">
                                            <div className="flex justify-between">
                                                <dt className="text-gray-500">Search Base</dt>
                                                <dd className="text-gray-900 font-mono text-xs">{ldapConfig.users?.search_base}</dd>
                                            </div>
                                            <div className="flex justify-between">
                                                <dt className="text-gray-500">Filter</dt>
                                                <dd className="text-gray-900 font-mono text-xs">{ldapConfig.users?.search_filter}</dd>
                                            </div>
                                        </dl>
                                    </div>
                                    <div>
                                        <h4 className="font-medium text-gray-900 mb-3">Organizations</h4>
                                        <dl className="space-y-2 text-sm">
                                            <div className="flex justify-between">
                                                <dt className="text-gray-500">Enabled</dt>
                                                <dd className="text-gray-900">{ldapConfig.organizations?.enabled ? 'Yes' : 'No'}</dd>
                                            </div>
                                        </dl>
                                    </div>
                                    <div>
                                        <h4 className="font-medium text-gray-900 mb-3">Teams</h4>
                                        <dl className="space-y-2 text-sm">
                                            <div className="flex justify-between">
                                                <dt className="text-gray-500">Enabled</dt>
                                                <dd className="text-gray-900">{ldapConfig.teams?.enabled ? 'Yes' : 'No'}</dd>
                                            </div>
                                        </dl>
                                    </div>
                                </div>
                            )}
                        </div>
                    </div>

                    {/* Sync History */}
                    <div className="bg-white rounded-lg shadow-sm border border-gray-200 overflow-hidden">
                        <div className="px-6 py-4 border-b border-gray-200 bg-gray-50">
                            <h2 className="text-lg font-semibold text-gray-900">Sync History</h2>
                            <p className="text-sm text-gray-500 mt-1">Click a row to view sync details</p>
                        </div>
                        <div className="overflow-x-auto">
                            <table className="w-full">
                                <thead className="bg-gray-50">
                                    <tr>
                                        <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
                                        <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Type</th>
                                        <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Started</th>
                                        <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Processed</th>
                                        <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
                                        <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Updated</th>
                                        <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Failed</th>
                                    </tr>
                                </thead>
                                <tbody className="divide-y divide-gray-200">
                                    {syncLogs.length === 0 ? (
                                        <tr>
                                            <td colSpan={7} className="px-6 py-8 text-center text-gray-500">No sync history yet</td>
                                        </tr>
                                    ) : (
                                        syncLogs.map((log) => (
                                            <React.Fragment key={log.id}>
                                                <tr
                                                    className={`hover:bg-gray-50 cursor-pointer transition-colors ${expandedLogId === log.id ? 'bg-blue-50' : ''}`}
                                                    onClick={() => toggleSyncDetails(log.id)}
                                                >
                                                    <td className="px-6 py-4 whitespace-nowrap">
                                                        <div className="flex items-center gap-2">
                                                            {getStatusIcon(log.status)}
                                                            <span className="text-sm capitalize">{log.status}</span>
                                                            {expandedLogId === log.id ? '▼' : '▶'}
                                                        </div>
                                                    </td>
                                                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-900 capitalize">{log.sync_type}</td>
                                                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{new Date(log.started_at).toLocaleString()}</td>
                                                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-900">{log.items_processed}</td>
                                                    <td className="px-6 py-4 whitespace-nowrap text-sm text-green-600">{log.items_created}</td>
                                                    <td className="px-6 py-4 whitespace-nowrap text-sm text-blue-600">{log.items_updated}</td>
                                                    <td className="px-6 py-4 whitespace-nowrap text-sm text-red-600">{log.items_failed}</td>
                                                </tr>
                                                {expandedLogId === log.id && (
                                                    <tr>
                                                        <td colSpan={7} className="px-6 py-4 bg-gray-50">
                                                            {expandedItems.length === 0 ? (
                                                                <div className="text-center py-4 text-gray-500">
                                                                    <RefreshCw className="animate-spin mx-auto mb-2" size={20} />
                                                                    Loading details...
                                                                </div>
                                                            ) : (
                                                                <div className="space-y-4">
                                                                    <h4 className="font-medium text-gray-900">Synced Items ({expandedItems.length})</h4>
                                                                    <div className="grid gap-3">
                                                                        {expandedItems.map((item) => (
                                                                            <div key={item.id} className={`border rounded-lg p-4 ${item.action === 'created' ? 'border-green-200 bg-green-50' : item.action === 'updated' ? 'border-blue-200 bg-blue-50' : 'border-gray-200 bg-white'}`}>
                                                                                <div className="flex items-center gap-3 mb-2">
                                                                                    <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${item.action === 'created' ? 'bg-green-100 text-green-800' :
                                                                                            item.action === 'updated' ? 'bg-blue-100 text-blue-800' :
                                                                                                item.action === 'unchanged' ? 'bg-gray-100 text-gray-800' :
                                                                                                    'bg-red-100 text-red-800'
                                                                                        }`}>
                                                                                        {item.action}
                                                                                    </span>
                                                                                    <span className="text-xs uppercase text-gray-500">{item.entity_type}</span>
                                                                                    <span className="font-medium text-gray-900">{item.entity_name}</span>
                                                                                </div>
                                                                                <div className="text-xs font-mono text-gray-500 mb-2">{item.ldap_dn}</div>
                                                                                {item.ldap_attributes && Object.keys(item.ldap_attributes).length > 0 && (
                                                                                    <div className="mt-2 border-t pt-2">
                                                                                        <div className="text-xs font-medium text-gray-700 mb-1">LDAP Attributes:</div>
                                                                                        <div className="grid grid-cols-2 gap-1 text-xs">
                                                                                            {Object.entries(item.ldap_attributes).map(([key, values]) => (
                                                                                                <div key={key} className="flex">
                                                                                                    <span className="font-mono text-gray-600 mr-1">{key}:</span>
                                                                                                    <span className="text-gray-900">{Array.isArray(values) ? values.join(', ') : values}</span>
                                                                                                </div>
                                                                                            ))}
                                                                                        </div>
                                                                                    </div>
                                                                                )}
                                                                            </div>
                                                                        ))}
                                                                    </div>
                                                                </div>
                                                            )}
                                                        </td>
                                                    </tr>
                                                )}
                                            </React.Fragment>
                                        ))
                                    )}
                                </tbody>
                            </table>
                        </div>
                    </div>
                </div>
            )}

            {/* SAML Tab */}
            {activeTab === 'saml' && (
                <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-8">
                    <div className="text-center">
                        <Key className="mx-auto text-gray-400 mb-4" size={48} />
                        <h3 className="text-lg font-medium text-gray-900 mb-2">SAML / SSO Configuration</h3>
                        <p className="text-gray-600 mb-6">Configure SAML 2.0 Single Sign-On with your identity provider</p>
                        <div className="bg-blue-50 border border-blue-200 rounded-md p-4 text-left max-w-xl mx-auto">
                            <h4 className="font-medium text-blue-800 mb-2">Coming Soon</h4>
                            <p className="text-sm text-blue-700">
                                SAML integration is available in the Go backend. UI configuration coming in the next release.
                                For now, configure via environment variables:
                            </p>
                            <ul className="mt-2 text-sm text-blue-700 list-disc list-inside">
                                <li>SAML_IDP_METADATA_URL</li>
                                <li>SAML_SP_ENTITY_ID</li>
                                <li>SAML_SP_ACS_URL</li>
                            </ul>
                        </div>
                    </div>
                </div>
            )}

            {/* GitHub Tab */}
            {activeTab === 'github' && (
                <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-8">
                    <div className="text-center">
                        <Github className="mx-auto text-gray-400 mb-4" size={48} />
                        <h3 className="text-lg font-medium text-gray-900 mb-2">GitHub Authentication</h3>
                        <p className="text-gray-600 mb-6">Allow users to sign in with their GitHub accounts</p>
                        <div className="bg-blue-50 border border-blue-200 rounded-md p-4 text-left max-w-xl mx-auto">
                            <h4 className="font-medium text-blue-800 mb-2">Coming Soon</h4>
                            <p className="text-sm text-blue-700">
                                GitHub OAuth integration is planned. Configure via environment variables:
                            </p>
                            <ul className="mt-2 text-sm text-blue-700 list-disc list-inside">
                                <li>GITHUB_CLIENT_ID</li>
                                <li>GITHUB_CLIENT_SECRET</li>
                                <li>GITHUB_ORG (optional - restrict to org members)</li>
                            </ul>
                        </div>
                    </div>
                </div>
            )}

            {/* OAuth2 Tab */}
            {activeTab === 'oauth2' && (
                <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-8">
                    <div className="text-center">
                        <Building className="mx-auto text-gray-400 mb-4" size={48} />
                        <h3 className="text-lg font-medium text-gray-900 mb-2">OAuth2 / OpenID Connect</h3>
                        <p className="text-gray-600 mb-6">Configure generic OAuth2 or OIDC authentication</p>
                        <div className="bg-blue-50 border border-blue-200 rounded-md p-4 text-left max-w-xl mx-auto">
                            <h4 className="font-medium text-blue-800 mb-2">Coming Soon</h4>
                            <p className="text-sm text-blue-700">
                                Generic OAuth2/OIDC support is planned. Configure via environment variables:
                            </p>
                            <ul className="mt-2 text-sm text-blue-700 list-disc list-inside">
                                <li>OAUTH2_CLIENT_ID</li>
                                <li>OAUTH2_CLIENT_SECRET</li>
                                <li>OAUTH2_AUTH_URL</li>
                                <li>OAUTH2_TOKEN_URL</li>
                                <li>OAUTH2_USERINFO_URL</li>
                            </ul>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
};

export default AuthProvidersPage;
