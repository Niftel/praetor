import React, { useEffect, useState, useMemo } from 'react';
import { api } from '../services/api';
import { Job } from '../types';
import Card from '../components/ui/Card';
import Badge from '../components/ui/Badge';
import { PageSpinner } from '../components/ui/PageSpinner';
import { CheckCircle, XCircle, Clock } from 'lucide-react';
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';

const DashboardPage = () => {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.getJobs()
      .then(data => setJobs(data || []))
      .catch(err => console.error(err))
      .finally(() => setLoading(false));
  }, []);

  const totalJobs = jobs.length;
  const successfulJobs = jobs.filter(j => j.status === 'successful').length;
  const failedJobs = jobs.filter(j => j.status === 'failed').length;
  const successRate = totalJobs > 0 ? Math.round((successfulJobs / totalJobs) * 100) : 0;

  // Real trend: bucket jobs by day over the last 7 days using started_at.
  const chartData = useMemo(() => {
    const days: { key: string; name: string; success: number; failed: number }[] = [];
    const now = new Date();
    for (let i = 6; i >= 0; i--) {
      const d = new Date(now);
      d.setDate(now.getDate() - i);
      days.push({
        key: d.toISOString().slice(0, 10),
        name: d.toLocaleDateString(undefined, { weekday: 'short' }),
        success: 0,
        failed: 0,
      });
    }
    const byKey = new Map(days.map(d => [d.key, d]));
    for (const j of jobs) {
      if (!j.started_at) continue;
      const bucket = byKey.get(new Date(j.started_at).toISOString().slice(0, 10));
      if (!bucket) continue;
      if (j.status === 'successful') bucket.success++;
      else if (j.status === 'failed') bucket.failed++;
    }
    return days;
  }, [jobs]);

  if (loading) return <PageSpinner />;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <Card className="border-l-4 border-l-blue-500">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-gray-500">Total Jobs Ran</p>
              <p className="text-3xl font-bold text-gray-900 mt-1">{totalJobs}</p>
            </div>
            <div className="p-3 bg-blue-50 rounded-full">
              <Clock className="w-6 h-6 text-blue-600" />
            </div>
          </div>
        </Card>

        <Card className="border-l-4 border-l-green-500">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-gray-500">Success Rate</p>
              <p className="text-3xl font-bold text-gray-900 mt-1">{successRate}%</p>
            </div>
            <div className="p-3 bg-green-50 rounded-full">
              <CheckCircle className="w-6 h-6 text-green-600" />
            </div>
          </div>
        </Card>

        <Card className="border-l-4 border-l-red-500">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-gray-500">Failed Jobs</p>
              <p className="text-3xl font-bold text-gray-900 mt-1">{failedJobs}</p>
            </div>
            <div className="p-3 bg-red-50 rounded-full">
              <XCircle className="w-6 h-6 text-red-600" />
            </div>
          </div>
        </Card>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2">
          <Card title="Job Execution Trends">
            <div className="h-72 w-full">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={chartData}>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} />
                  <XAxis dataKey="name" axisLine={false} tickLine={false} />
                  <YAxis axisLine={false} tickLine={false} />
                  <Tooltip
                    contentStyle={{ borderRadius: '8px', border: 'none', boxShadow: '0 4px 6px -1px rgb(0 0 0 / 0.1)' }}
                  />
                  <Bar dataKey="success" fill="#10b981" radius={[4, 4, 0, 0]} />
                  <Bar dataKey="failed" fill="#ef4444" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </Card>
        </div>

        <div className="lg:col-span-1">
          <Card title="Recent Activity">
            <div className="space-y-4">
              {jobs.slice(0, 5).map(job => (
                <div key={job.id} className="flex items-center justify-between p-3 hover:bg-gray-50 rounded-lg transition-colors border border-transparent hover:border-gray-100">
                  <div className="flex items-center gap-3">
                    <div className={`w-2 h-2 rounded-full ${job.status === 'successful' ? 'bg-green-500' : 'bg-red-500'}`} />
                    <div>
                      <p className="text-sm font-medium text-gray-900">{job.name}</p>
                      <p className="text-xs text-gray-500">{job.started_at ? new Date(job.started_at).toLocaleDateString() : '-'}</p>
                    </div>
                  </div>
                  <Badge variant={job.status === 'successful' ? 'success' : 'error'}>
                    {job.status}
                  </Badge>
                </div>
              ))}
            </div>
          </Card>
        </div>
      </div>
    </div>
  );
};

export default DashboardPage;