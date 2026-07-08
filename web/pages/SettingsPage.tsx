import React from 'react';
import { Link } from 'react-router-dom';
import { Key, ChevronRight } from 'lucide-react';

interface SettingsCategory {
    name: string;
    description: string;
    path: string;
    icon: React.ReactNode;
}

// Settings landing page (AWX-style). Each category links to a dedicated
// sub-page under /settings. Add new entries here as more settings areas land
// (jobs, system, notifications, …).
const categories: SettingsCategory[] = [
    {
        name: 'Auth Settings',
        description: 'LDAP / SAML / GitHub / OAuth2 — how users sign in and how directory groups map to Praetor roles.',
        path: '/settings/auth-providers',
        icon: <Key size={22} />,
    },
];

const SettingsPage: React.FC = () => {
    return (
        <div className="space-y-6">
            <div>
                <h1 className="text-2xl font-bold text-gray-900">Settings</h1>
                <p className="text-gray-600 mt-1">Configure how Praetor authenticates users and runs the platform.</p>
            </div>

            <div className="grid gap-4 md:grid-cols-2">
                {categories.map((c) => (
                    <Link
                        key={c.path}
                        to={c.path}
                        className="group flex items-start gap-4 bg-white rounded-lg shadow-sm border border-gray-200 p-5 hover:border-brand-400 hover:shadow-md transition-all"
                    >
                        <div className="shrink-0 w-11 h-11 rounded-md bg-brand-50 text-brand-600 flex items-center justify-center">
                            {c.icon}
                        </div>
                        <div className="flex-1 min-w-0">
                            <div className="flex items-center justify-between">
                                <h2 className="text-base font-semibold text-gray-900">{c.name}</h2>
                                <ChevronRight size={18} className="text-gray-300 group-hover:text-brand-500 transition-colors" />
                            </div>
                            <p className="text-sm text-gray-500 mt-1">{c.description}</p>
                        </div>
                    </Link>
                ))}
            </div>
        </div>
    );
};

export default SettingsPage;
