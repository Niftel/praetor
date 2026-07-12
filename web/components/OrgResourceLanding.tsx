import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api, unwrap } from '../services/api';
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
        <div className="p-8 max-w-[1160px] mx-auto bg-bg text-ink">
            <div className="mb-6">
                <h1 className="text-[21px] font-semibold tracking-tight">{title}</h1>
                <p className="text-[13px] text-mut mt-1">Choose an organization to view and manage its {title.toLowerCase()}.</p>
            </div>

            {orgs.length === 0 ? (
                <div className="rounded-xl border border-line bg-panel p-8 text-center text-mut text-sm">
                    You're not a member of any organization yet. Ask an administrator to add you to one.
                </div>
            ) : (
                <div className="grid gap-3.5 md:grid-cols-2 lg:grid-cols-3">
                    {orgs.map((o) => (
                        <Link
                            key={o.id}
                            to={`${basePath}/org/${o.id}`}
                            className="group flex items-start gap-4 bg-panel rounded-xl border border-line p-5
                                transition-[border-color,transform] duration-200 hover:-translate-y-0.5 hover:border-line2"
                        >
                            <div className="shrink-0 w-11 h-11 rounded-lg bg-acc/10 text-acc2 grid place-items-center ring-1 ring-acc/15">
                                <Building2 size={21} />
                            </div>
                            <div className="flex-1 min-w-0">
                                <div className="flex items-center justify-between gap-2">
                                    <h2 className="text-[15px] font-semibold tracking-tight text-ink truncate">{o.name}</h2>
                                    <ChevronRight size={18} className="text-faint group-hover:text-acc2 group-hover:translate-x-0.5 transition-all shrink-0" />
                                </div>
                                {o.description && <p className="text-[13px] text-mut mt-0.5 truncate">{o.description}</p>}
                                <p className="font-mono text-[11px] text-acc2 mt-2 tabular-nums">{plural(counts[o.id] || 0)}</p>
                            </div>
                        </Link>
                    ))}
                </div>
            )}
        </div>
    );
};

export default OrgResourceLanding;
