import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api, unwrap } from '../services/api';
import Card from './ui/Card';
import { PageSpinner } from './ui/PageSpinner';
import { Building2, ChevronRight } from 'lucide-react';

interface Org {
    id: number;
    name: string;
    description?: string;
}

interface OrgResourceLandingProps {
    title: string;                    // e.g. "Projects"
    basePath: string;                 // e.g. "/projects" -> cards link to /projects/org/:id
    unit: string;                     // singular noun for the count, e.g. "project"
    // Returns the resource list (each item carrying organization_id) so we can
    // show a per-org count. Optional — omit for resources without a flat list.
    fetchItems?: () => Promise<any>;
}

// Org-first landing for a resource section: shows the organizations the user
// belongs to as cards (with a per-org count), each drilling into that org's
// resources. Replaces the flat list + org-dropdown create pattern.
const OrgResourceLanding: React.FC<OrgResourceLandingProps> = ({ title, basePath, unit, fetchItems }) => {
    const [orgs, setOrgs] = useState<Org[]>([]);
    const [counts, setCounts] = useState<Record<number, number>>({});
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        (async () => {
            try {
                const [orgData, items] = await Promise.all([
                    api.getOrganizations(),
                    fetchItems ? fetchItems().catch(() => []) : Promise.resolve([]),
                ]);
                setOrgs(unwrap<Org>(orgData));
                const c: Record<number, number> = {};
                for (const it of unwrap<{ organization_id: number }>(items)) {
                    if (it.organization_id != null) c[it.organization_id] = (c[it.organization_id] || 0) + 1;
                }
                setCounts(c);
            } catch (e) {
                console.error('Failed to load organizations', e);
            } finally {
                setLoading(false);
            }
        })();
    }, [fetchItems]);

    if (loading) return <PageSpinner />;

    const plural = (n: number) => `${n} ${unit}${n === 1 ? '' : 's'}`;

    return (
        <div className="space-y-6">
            <div>
                <h1 className="text-2xl font-bold text-gray-900">{title}</h1>
                <p className="text-gray-600 mt-1">Choose an organization to view and manage its {title.toLowerCase()}.</p>
            </div>

            {orgs.length === 0 ? (
                <Card>
                    <p className="text-gray-500 text-center py-8">
                        You're not a member of any organization yet. Ask an administrator to add you to one.
                    </p>
                </Card>
            ) : (
                <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                    {orgs.map((o) => (
                        <Link
                            key={o.id}
                            to={`${basePath}/org/${o.id}`}
                            className="group flex items-start gap-4 bg-white rounded-lg shadow-sm border border-gray-200 p-5 hover:border-brand-400 hover:shadow-md transition-all"
                        >
                            <div className="shrink-0 w-11 h-11 rounded-md bg-brand-50 text-brand-600 flex items-center justify-center">
                                <Building2 size={22} />
                            </div>
                            <div className="flex-1 min-w-0">
                                <div className="flex items-center justify-between">
                                    <h2 className="text-base font-semibold text-gray-900 truncate">{o.name}</h2>
                                    <ChevronRight size={18} className="text-gray-300 group-hover:text-brand-500 transition-colors shrink-0" />
                                </div>
                                {o.description && <p className="text-sm text-gray-500 mt-0.5 truncate">{o.description}</p>}
                                <p className="text-xs font-medium text-brand-600 mt-2">{plural(counts[o.id] || 0)}</p>
                            </div>
                        </Link>
                    ))}
                </div>
            )}
        </div>
    );
};

export default OrgResourceLanding;
