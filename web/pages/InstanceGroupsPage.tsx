import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { InstanceGroup } from '../types';
import { Plus, Trash2, Layers } from 'lucide-react';
import Card from '../components/ui/Card';

const InstanceGroupsPage = () => {
    const [groups, setGroups] = useState<InstanceGroup[]>([]);
    const [loading, setLoading] = useState(true);
    const [showModal, setShowModal] = useState(false);
    const [newGroup, setNewGroup] = useState<Partial<InstanceGroup>>({
        name: '',
    });

    const loadData = () => {
        setLoading(true);
        api.getInstanceGroups()
            .then(data => setGroups(data || []))
            .catch(err => console.error(err))
            .finally(() => setLoading(false));
    };

    useEffect(() => {
        loadData();
    }, []);

    const handleCreate = async () => {
        try {
            await api.createInstanceGroup(newGroup);
            setShowModal(false);
            setNewGroup({ name: '' });
            loadData();
        } catch (error) {
            console.error(error);
            alert('Failed to create group');
        }
    };

    const handleDelete = async (id: number) => {
        if (confirm('Are you sure you want to delete this group?')) {
            try {
                await api.deleteInstanceGroup(id);
                loadData();
            } catch (error) {
                console.error(error);
            }
        }
    };

    return (
        <div className="space-y-6">
            <div className="flex justify-between items-center">
                <h1 className="text-2xl font-bold text-gray-900">Instance Groups</h1>
                <button
                    onClick={() => setShowModal(true)}
                    className="bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700 flex items-center gap-2"
                >
                    <Plus size={20} /> Create Group
                </button>
            </div>

            <Card>
                <div className="overflow-x-auto">
                    <table className="min-w-full divide-y divide-gray-200">
                        <thead className="bg-gray-50">
                            <tr>
                                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Name</th>
                                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Created At</th>
                                <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Actions</th>
                            </tr>
                        </thead>
                        <tbody className="bg-white divide-y divide-gray-200">
                            {groups.map((g) => (
                                <tr key={g.id}>
                                    <td className="px-6 py-4 whitespace-nowrap">
                                        <div className="flex items-center">
                                            <Layers className="h-5 w-5 text-gray-400 mr-2" />
                                            <div className="text-sm font-medium text-gray-900">{g.name}</div>
                                        </div>
                                    </td>
                                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{new Date(g.created_at).toLocaleDateString()}</td>
                                    <td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                                        <button onClick={() => handleDelete(g.id)} className="text-red-600 hover:text-red-900">
                                            <Trash2 size={18} />
                                        </button>
                                    </td>
                                </tr>
                            ))}
                            {groups.length === 0 && !loading && (
                                <tr>
                                    <td colSpan={3} className="px-6 py-4 text-center text-sm text-gray-500">
                                        No instance groups found.
                                    </td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>
            </Card>

            {showModal && (
                <div className="fixed inset-0 bg-gray-600 bg-opacity-50 overflow-y-auto h-full w-full flex items-center justify-center">
                    <div className="bg-white p-5 rounded-lg shadow-xl w-96">
                        <h3 className="text-lg font-bold mb-4">Create Instance Group</h3>
                        <div className="mb-4">
                            <label className="block text-gray-700 text-sm font-bold mb-2">Name</label>
                            <input
                                className="shadow appearance-none border rounded w-full py-2 px-3 text-gray-700 leading-tight focus:outline-none focus:shadow-outline"
                                value={newGroup.name}
                                onChange={(e) => setNewGroup({ ...newGroup, name: e.target.value })}
                            />
                        </div>
                        <div className="flex justify-end gap-2">
                            <button onClick={() => setShowModal(false)} className="bg-gray-300 hover:bg-gray-400 text-gray-800 font-bold py-2 px-4 rounded">Cancel</button>
                            <button onClick={handleCreate} className="bg-blue-500 hover:bg-blue-700 text-white font-bold py-2 px-4 rounded">Create</button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
};

export default InstanceGroupsPage;
