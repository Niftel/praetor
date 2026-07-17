import { useEffect, useState } from 'react';
import { api, ResourceCapabilities } from '../services/api';

const denied: ResourceCapabilities = {
  view: false,
  manage: false,
  use: false,
  execute: false,
  update: false,
  approve: false,
};

export function useCapabilities(contentType: string, objectId: number | null | undefined) {
  const [capabilities, setCapabilities] = useState<ResourceCapabilities>(denied);
  const [loading, setLoading] = useState(!!objectId);

  useEffect(() => {
    let active = true;
    if (!objectId) {
      setCapabilities(denied);
      setLoading(false);
      return () => { active = false; };
    }
    setCapabilities(denied);
    setLoading(true);
    api.getCapabilities(contentType, objectId)
      .then(value => { if (active) setCapabilities({ ...denied, ...value }); })
      .catch(() => { if (active) setCapabilities(denied); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [contentType, objectId]);

  return { capabilities, loading };
}
