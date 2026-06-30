import React, { useEffect, useState } from 'react';
import { api } from '../services/api';
import Card from '../components/ui/Card';
import { RefreshCw } from 'lucide-react';

const ActivityPage = () => {
  const [entries, setEntries] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  const load = () => {
    setLoading(true);
    api.getActivityStream(200)
      .then(d => setEntries(d || []))
      .catch(() => setEntries([]))
      .finally(() => setLoading(false));
  };
  useEffect(() => { load(); }, []);

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Activity</h1>
          <p className="text-sm text-gray-500 mt-1">Audit log of who changed or launched what (superuser / auditor only)</p>
        </div>
        <button onClick={load} disabled={loading} className="text-gray-600 hover:text-gray-900 p-2 rounded-lg hover:bg-gray-100" title="Refresh">
          <RefreshCw size={20} className={loading ? 'animate-spin' : ''} />
        </button>
      </div>
      <Card className="overflow-hidden">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">When</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">User</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Action</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Resource</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100">
            {entries.map(e => (
              <tr key={e.id} className="hover:bg-gray-50">
                <td className="px-4 py-2 text-sm text-gray-500 whitespace-nowrap">{new Date(e.created_at).toLocaleString()}</td>
                <td className="px-4 py-2 text-sm text-gray-900">{e.username || '—'}</td>
                <td className="px-4 py-2 text-sm"><span className="px-2 py-0.5 rounded text-xs bg-gray-100 text-gray-700">{e.action}</span></td>
                <td className="px-4 py-2 text-sm text-gray-700 font-mono">{e.resource_type}{e.resource_id ? `/${e.resource_id}` : ''}</td>
                <td className="px-4 py-2 text-sm"><span className={e.status_code < 300 ? 'text-green-600' : 'text-red-600'}>{e.status_code}</span></td>
              </tr>
            ))}
            {entries.length === 0 && !loading && (
              <tr><td colSpan={5} className="px-4 py-6 text-center text-sm text-gray-500">No activity recorded.</td></tr>
            )}
          </tbody>
        </table>
      </Card>
    </div>
  );
};

export default ActivityPage;
