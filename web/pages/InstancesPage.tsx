import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Instance } from '../types';
import { Settings, Activity, Clock, RefreshCw, Server, Boxes } from 'lucide-react';
import Card from '../components/ui/Card';

interface RunnerHost {
    id: number;
    name: string;
    inventory_id: number;
    inventory_name: string;
    enabled: boolean;
    is_runner_host: boolean;
    runner_healthy: boolean;
    runner_last_seen: string | null;
}

// Health badge derived from heartbeat recency, shared by runner hosts and
// control-plane instances so they read consistently.
const healthBadge = (lastSeen?: string | null) => {
    if (!lastSeen) {
        return <span className="text-gray-400 text-sm">No heartbeat yet</span>;
    }
    const diffSec = Math.floor((Date.now() - new Date(lastSeen).getTime()) / 1000);
    if (diffSec < 60) {
        return (
            <span className="flex items-center text-green-600 text-sm">
                <Activity className="h-4 w-4 mr-1 animate-pulse" />
                Healthy ({diffSec}s ago)
            </span>
        );
    }
    if (diffSec < 300) {
        return (
            <span className="flex items-center text-yellow-600 text-sm">
                <Clock className="h-4 w-4 mr-1" />
                Warning ({Math.floor(diffSec / 60)}m ago)
            </span>
        );
    }
    return (
        <span className="flex items-center text-red-600 text-sm">
            <Clock className="h-4 w-4 mr-1" />
            Unhealthy ({Math.floor(diffSec / 60)}m ago)
        </span>
    );
};

const InstancesPage = () => {
    const [instances, setInstances] = useState<Instance[]>([]);
    const [hosts, setHosts] = useState<RunnerHost[]>([]);
    const [loading, setLoading] = useState(true);

    const loadData = () => {
        setLoading(true);
        Promise.all([
            api.getInstances()
                .then((d: Instance[]) => (d || []).filter((i) => i.instance_type === 'executor'))
                .catch(() => [] as Instance[]),
            api.getRunnerHosts()
                .then((d: RunnerHost[]) => d || [])
                .catch(() => [] as RunnerHost[]),
        ])
            .then(([inst, rh]) => { setInstances(inst); setHosts(rh); })
            .finally(() => setLoading(false));
    };

    useEffect(() => {
        loadData();
        // Refresh every 30s to surface heartbeat updates.
        const interval = setInterval(loadData, 30000);
        return () => clearInterval(interval);
    }, []);

    return (
        <div className="space-y-8">
            <div className="flex justify-between items-center">
                <div>
                    <h1 className="text-2xl font-bold text-gray-900">Infrastructure</h1>
                    <p className="text-sm text-gray-500 mt-1">
                        Execution hosts where jobs run, and the control-plane nodes that dispatch them
                    </p>
                </div>
                <button
                    onClick={loadData}
                    disabled={loading}
                    className="text-gray-600 hover:text-gray-900 p-2 rounded-lg hover:bg-gray-100"
                    title="Refresh"
                >
                    <RefreshCw size={20} className={loading ? 'animate-spin' : ''} />
                </button>
            </div>

            {/* Execution hosts — where jobs actually run */}
            <section>
                <div className="flex items-center mb-3">
                    <Server className="h-5 w-5 text-gray-700 mr-2" />
                    <h2 className="text-lg font-semibold text-gray-900">Execution hosts</h2>
                    <span className="ml-2 text-sm text-gray-400">{hosts.length}</span>
                </div>
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                    {hosts.map((h) => (
                        <Card key={h.id} className="p-4">
                            <div className="flex items-start justify-between">
                                <div className="flex items-center">
                                    <div className="bg-blue-100 p-2 rounded-lg mr-3">
                                        <Server className="h-6 w-6 text-blue-600" />
                                    </div>
                                    <div>
                                        <h3 className="font-semibold text-gray-900">{h.name}</h3>
                                        <p className="text-xs text-gray-500">{h.inventory_name}</p>
                                    </div>
                                </div>
                                <span className={`px-2 py-1 text-xs font-medium rounded-full ${h.enabled
                                    ? 'bg-green-100 text-green-800'
                                    : 'bg-gray-100 text-gray-600'
                                    }`}>
                                    {h.enabled ? 'Enabled' : 'Disabled'}
                                </span>
                            </div>
                            <div className="mt-4 pt-4 border-t border-gray-100">
                                <div className="flex justify-between items-center">
                                    <span className="text-sm text-gray-500">Health</span>
                                    {healthBadge(h.runner_last_seen)}
                                </div>
                                <div className="flex justify-between items-center mt-2">
                                    <span className="text-sm text-gray-500">Role</span>
                                    <span className="text-sm font-medium text-gray-900">Runner host</span>
                                </div>
                            </div>
                        </Card>
                    ))}
                    {hosts.length === 0 && !loading && (
                        <div className="col-span-full">
                            <Card className="p-8 text-center">
                                <Server className="h-12 w-12 text-gray-300 mx-auto mb-4" />
                                <h3 className="text-lg font-medium text-gray-900 mb-2">No runner hosts</h3>
                                <p className="text-sm text-gray-500">
                                    Designate a host as a runner on an inventory to see it here. Health
                                    appears once it runs a job and starts heartbeating.
                                </p>
                            </Card>
                        </div>
                    )}
                </div>
            </section>

            {/* Control plane — the nodes that bootstrap and dispatch jobs */}
            <section>
                <div className="flex items-center mb-3">
                    <Boxes className="h-5 w-5 text-gray-700 mr-2" />
                    <h2 className="text-lg font-semibold text-gray-900">Control plane</h2>
                    <span className="ml-2 text-sm text-gray-400">{instances.length}</span>
                </div>
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                    {instances.map((inst) => (
                        <Card key={inst.id} className="p-4">
                            <div className="flex items-start justify-between">
                                <div className="flex items-center">
                                    <div className="bg-purple-100 p-2 rounded-lg mr-3">
                                        <Settings className="h-6 w-6 text-purple-600" />
                                    </div>
                                    <div>
                                        <h3 className="font-mono text-sm font-semibold text-gray-900">{inst.hostname}</h3>
                                        <p className="text-xs text-gray-500">{inst.instance_type || 'executor'}</p>
                                    </div>
                                </div>
                                <span className={`px-2 py-1 text-xs font-medium rounded-full ${inst.enabled
                                    ? 'bg-green-100 text-green-800'
                                    : 'bg-gray-100 text-gray-600'
                                    }`}>
                                    {inst.enabled ? 'Active' : 'Disabled'}
                                </span>
                            </div>
                            <div className="mt-4 pt-4 border-t border-gray-100">
                                <div className="flex justify-between items-center">
                                    <span className="text-sm text-gray-500">Health</span>
                                    {healthBadge(inst.last_heartbeat)}
                                </div>
                                <div className="flex justify-between items-center mt-2">
                                    <span className="text-sm text-gray-500">Capacity</span>
                                    <span className="text-sm font-medium text-gray-900">{inst.capacity}</span>
                                </div>
                            </div>
                        </Card>
                    ))}
                    {instances.length === 0 && !loading && (
                        <div className="col-span-full">
                            <Card className="p-8 text-center">
                                <Settings className="h-12 w-12 text-gray-300 mx-auto mb-4" />
                                <h3 className="text-lg font-medium text-gray-900 mb-2">No control-plane nodes</h3>
                                <p className="text-sm text-gray-500">
                                    Executors register automatically when they start; dead nodes are reaped
                                    once they stop heartbeating.
                                </p>
                            </Card>
                        </div>
                    )}
                </div>
            </section>
        </div>
    );
};

export default InstancesPage;
