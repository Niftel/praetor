import React, { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../services/api';
import { RefreshCw, CheckCircle, XCircle, AlertCircle, Key, Users, Building, Github } from 'lucide-react';

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
    group_type?: {
        type: string;
        search_base: string;
    };
    user_flags_by_group?: {
        is_superuser: string[] | null;
        is_system_auditor: string[] | null;
    };
    // Rendered as the raw AUTH_LDAP_*_MAP config: each role value is a group DN,
    // a list of DNs, or a bool ("all"/"none"), plus its remove_* flag.
    organization_map?: Record<string, Record<string, string | string[] | boolean>>;
    team_map?: Record<string, Record<string, string | string[] | boolean>>;
}

const AuthProvidersPage: React.FC = () => {
    const [activeTab, setActiveTab] = useState<'ldap' | 'saml' | 'github' | 'oauth2'>('ldap');
    const [ldapConfig, setLdapConfig] = useState<LdapConfig | null>(null);
    const [loading, setLoading] = useState(true);
    const [testing, setTesting] = useState(false);
    const [testResult, setTestResult] = useState<{ success: boolean; message?: string; error?: string } | null>(null);

    useEffect(() => {
        loadData();
    }, []);

    const loadData = async () => {
        setLoading(true);
        try {
            const configData = await api.getLdapConfig();
            setLdapConfig(configData);
        } catch (error) {
            console.error('Failed to load LDAP data:', error);
        } finally {
            setLoading(false);
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

    const tabs = [
        { id: 'ldap', name: 'LDAP', icon: <Users size={16} /> },
        { id: 'saml', name: 'SAML', icon: <Key size={16} /> },
        { id: 'github', name: 'GitHub', icon: <Github size={16} /> },
        { id: 'oauth2', name: 'OAuth2 / OIDC', icon: <Building size={16} /> },
    ];

    // Render a labelled list of group DNs, or an em dash when empty.
    const dnList = (dns?: string[] | null) => {
        if (!dns || dns.length === 0) return <span className="text-gray-400">—</span>;
        return (
            <div className="space-y-0.5">
                {dns.map((dn) => (
                    <div key={dn} className="text-gray-900 font-mono text-xs">{dn}</div>
                ))}
            </div>
        );
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center h-64">
                <RefreshCw className="animate-spin text-brand-500" size={32} />
            </div>
        );
    }

    const orgMap = ldapConfig?.organization_map || {};
    const teamMap = ldapConfig?.team_map || {};

    return (
        <div className="p-6 space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <Link to="/settings" className="text-sm text-gray-500 hover:text-brand-600">← Settings</Link>
                    <h1 className="text-2xl font-bold text-gray-900 mt-1">Auth Settings</h1>
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
                                    <div>
                                        <h2 className="text-lg font-semibold text-gray-900">LDAP Configuration</h2>
                                        <p className="text-sm text-gray-500">Group→role mapping is applied at login (read-only here).</p>
                                    </div>
                                </div>
                                <button
                                    onClick={handleTestConnection}
                                    disabled={!ldapConfig?.configured || testing}
                                    className="px-4 py-2 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-md hover:bg-gray-50 disabled:opacity-50 disabled:cursor-not-allowed"
                                >
                                    {testing ? 'Testing...' : 'Test Connection'}
                                </button>
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
                                        See <code className="bg-gray-100 px-1 rounded">deployments/ldap/ldap-config.yaml</code> for an example.
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
                                <div className="space-y-6">
                                    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                                        <div>
                                            <h4 className="font-medium text-gray-900 mb-3">Server</h4>
                                            <dl className="space-y-2 text-sm">
                                                <div className="flex justify-between gap-4">
                                                    <dt className="text-gray-500">URL</dt>
                                                    <dd className="text-gray-900 font-mono">{ldapConfig.server?.url}</dd>
                                                </div>
                                                <div className="flex justify-between gap-4">
                                                    <dt className="text-gray-500">Bind DN</dt>
                                                    <dd className="text-gray-900 font-mono text-xs">{ldapConfig.server?.bind_dn}</dd>
                                                </div>
                                                <div className="flex justify-between gap-4">
                                                    <dt className="text-gray-500">StartTLS</dt>
                                                    <dd className="text-gray-900">{ldapConfig.server?.start_tls ? 'Yes' : 'No'}</dd>
                                                </div>
                                            </dl>
                                        </div>
                                        <div>
                                            <h4 className="font-medium text-gray-900 mb-3">Users</h4>
                                            <dl className="space-y-2 text-sm">
                                                <div className="flex justify-between gap-4">
                                                    <dt className="text-gray-500">Search Base</dt>
                                                    <dd className="text-gray-900 font-mono text-xs">{ldapConfig.users?.search_base}</dd>
                                                </div>
                                                <div className="flex justify-between gap-4">
                                                    <dt className="text-gray-500">Filter</dt>
                                                    <dd className="text-gray-900 font-mono text-xs">{ldapConfig.users?.search_filter}</dd>
                                                </div>
                                            </dl>
                                        </div>
                                        <div>
                                            <h4 className="font-medium text-gray-900 mb-3">Group Membership</h4>
                                            <dl className="space-y-2 text-sm">
                                                <div className="flex justify-between gap-4">
                                                    <dt className="text-gray-500">Type</dt>
                                                    <dd className="text-gray-900 font-mono">{ldapConfig.group_type?.type || '—'}</dd>
                                                </div>
                                                <div className="flex justify-between gap-4">
                                                    <dt className="text-gray-500">Search Base</dt>
                                                    <dd className="text-gray-900 font-mono text-xs">{ldapConfig.group_type?.search_base || '—'}</dd>
                                                </div>
                                            </dl>
                                        </div>
                                        <div>
                                            <h4 className="font-medium text-gray-900 mb-3">Platform Flags by Group</h4>
                                            <dl className="space-y-2 text-sm">
                                                <div>
                                                    <dt className="text-gray-500 mb-1">Superuser</dt>
                                                    <dd>{dnList(ldapConfig.user_flags_by_group?.is_superuser)}</dd>
                                                </div>
                                                <div>
                                                    <dt className="text-gray-500 mb-1">System Auditor</dt>
                                                    <dd>{dnList(ldapConfig.user_flags_by_group?.is_system_auditor)}</dd>
                                                </div>
                                            </dl>
                                        </div>
                                    </div>

                                    {/* Organization map — shown as the raw AUTH_LDAP_ORGANIZATION_MAP
                                        query: every organization in one block. */}
                                    <div>
                                        <h4 className="font-medium text-gray-900 mb-3">Organization Map ({Object.keys(orgMap).length})</h4>
                                        {Object.keys(orgMap).length === 0 ? (
                                            <p className="text-sm text-gray-400">No organizations mapped.</p>
                                        ) : (
                                            <pre className="bg-gray-50 border border-gray-200 rounded-lg p-4 text-xs font-mono text-gray-800 overflow-x-auto whitespace-pre">
                                                {JSON.stringify(orgMap, null, 2)}
                                            </pre>
                                        )}
                                    </div>

                                    {/* Team map — raw AUTH_LDAP_TEAM_MAP query, all teams in one block. */}
                                    <div>
                                        <h4 className="font-medium text-gray-900 mb-3">Team Map ({Object.keys(teamMap).length})</h4>
                                        {Object.keys(teamMap).length === 0 ? (
                                            <p className="text-sm text-gray-400">No teams mapped.</p>
                                        ) : (
                                            <pre className="bg-gray-50 border border-gray-200 rounded-lg p-4 text-xs font-mono text-gray-800 overflow-x-auto whitespace-pre">
                                                {JSON.stringify(teamMap, null, 2)}
                                            </pre>
                                        )}
                                    </div>
                                </div>
                            )}
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
