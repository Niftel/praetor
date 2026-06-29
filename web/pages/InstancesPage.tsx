import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Instance } from '../types';
import { Settings, Activity, Clock, RefreshCw } from 'lucide-react';
import Card from '../components/ui/Card';

const InstancesPage = () => {
    const [instances, setInstances] = useState<Instance[]>([]);
    const [loading, setLoading] = useState(true);

    const loadData = () => {
        setLoading(true);
        api.getInstances()
            .then(data => {
                // Filter to show only executors
                const executors = (data || []).filter(i => i.instance_type === 'executor');
                setInstances(executors);
            })
            .catch(err => console.error(err))
            .finally(() => setLoading(false));
    };

    useEffect(() => {
        loadData();
        // Refresh every 30s to show heartbeat updates
        const interval = setInterval(loadData, 30000);
        return () => clearInterval(interval);
    }, []);

    const getHealthStatus = (inst: Instance) => {
        if (!inst.last_heartbeat) {
            return <span className="text-gray-400 text-sm">No heartbeat</span>;
        }
        const lastBeat = new Date(inst.last_heartbeat);
        const now = new Date();
        const diffSec = Math.floor((now.getTime() - lastBeat.getTime()) / 1000);

        if (diffSec < 60) {
            return (
                <span className="flex items-center text-green-600 text-sm">
                    <Activity className="h-4 w-4 mr-1 animate-pulse" />
                    Healthy ({diffSec}s ago)
                </span>
            );
        } else if (diffSec < 300) {
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
                Unhealthy
            </span>
        );
    };

    return (
        <div className="space-y-6">
            <div className="flex justify-between items-center">
                <div>
                    <h1 className="text-2xl font-bold text-gray-900">Executors</h1>
                    <p className="text-sm text-gray-500 mt-1">Nodes that bootstrap and dispatch jobs to remote hosts</p>
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

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {instances.map((inst) => (
                    <Card key={inst.id} className="p-4">
                        <div className="flex items-start justify-between">
                            <div className="flex items-center">
                                <div className="bg-purple-100 p-2 rounded-lg mr-3">
                                    <Settings className="h-6 w-6 text-purple-600" />
                                </div>
                                <div>
                                    <h3 className="font-semibold text-gray-900">{inst.hostname}</h3>
                                    {inst.ip_address && (
                                        <p className="text-xs text-gray-500">{inst.ip_address}</p>
                                    )}
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
                                {getHealthStatus(inst)}
                            </div>
                            <div className="flex justify-between items-center mt-2">
                                <span className="text-sm text-gray-500">Capacity</span>
                                <span className="text-sm font-medium text-gray-900">{inst.capacity}</span>
                            </div>
                            {inst.version && (
                                <div className="flex justify-between items-center mt-2">
                                    <span className="text-sm text-gray-500">Version</span>
                                    <span className="text-sm text-gray-600">{inst.version}</span>
                                </div>
                            )}
                        </div>
                    </Card>
                ))}

                {instances.length === 0 && !loading && (
                    <div className="col-span-full">
                        <Card className="p-8 text-center">
                            <Settings className="h-12 w-12 text-gray-300 mx-auto mb-4" />
                            <h3 className="text-lg font-medium text-gray-900 mb-2">No executors registered</h3>
                            <p className="text-sm text-gray-500">
                                Executors automatically register when they start up.
                            </p>
                        </Card>
                    </div>
                )}
            </div>
        </div>
    );
};

export default InstancesPage;
