import React, { useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle2, Clock3, History, RefreshCw } from 'lucide-react';
import { api } from '../services/api';
import type { NotificationDelivery, NotificationDeliveryPage, NotificationDeliveryStatus } from '../types';
import { Badge, Button, EmptyState, ErrorState, LoadingState, Select } from './ui';

const STATUS_OPTIONS: Array<{ value: '' | NotificationDeliveryStatus; label: string }> = [
  { value: '', label: 'All statuses' },
  { value: 'pending', label: 'Pending' },
  { value: 'retrying', label: 'Retrying' },
  { value: 'sending', label: 'Sending' },
  { value: 'delivered', label: 'Delivered' },
  { value: 'failed', label: 'Failed' },
];

const statusBadge = (status: NotificationDeliveryStatus) => {
  if (status === 'delivered') return <Badge variant="success" dot>Delivered</Badge>;
  if (status === 'failed') return <Badge variant="error" dot>Failed</Badge>;
  if (status === 'retrying') return <Badge variant="warning" dot>Retrying</Badge>;
  if (status === 'sending') return <Badge variant="info" dot>Sending</Badge>;
  return <Badge variant="neutral" dot>Pending</Badge>;
};

const eventLabel = (value: string) => value.replaceAll('_', ' ');
const resourceLabel = (value: string) => value.replaceAll('_', ' ');

const NotificationDeliveryHistory: React.FC<{ organizationId: number }> = ({ organizationId }) => {
  const [status, setStatus] = useState<'' | NotificationDeliveryStatus>('');
  const [deliveries, setDeliveries] = useState<NotificationDelivery[]>([]);
  const [nextCursor, setNextCursor] = useState<number | undefined>();
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState('');

  const load = async (cursor?: number) => {
    if (!organizationId) {
      setDeliveries([]);
      setNextCursor(undefined);
      return;
    }
    cursor ? setLoadingMore(true) : setLoading(true);
    setError('');
    try {
      const page = await api.getNotificationDeliveries(organizationId, {
        status: status || undefined,
        cursor,
        limit: 25,
      }) as NotificationDeliveryPage;
      setDeliveries(current => cursor ? [...current, ...page.results] : page.results);
      setNextCursor(page.next_cursor);
    } catch (requestError: any) {
      setError(requestError.message || 'Notification delivery history could not be loaded.');
      if (!cursor) setDeliveries([]);
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  };

  useEffect(() => { void load(); }, [organizationId, status]);

  return (
    <section aria-labelledby="notification-delivery-history-heading" className="mt-8">
      <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 id="notification-delivery-history-heading" className="flex items-center gap-2 text-[15px] font-semibold text-ink">
            <History size={16} /> Delivery history
          </h2>
          <p className="mt-1 text-xs text-mut">Durable attempts and redacted failure diagnostics for this organization.</p>
        </div>
        <div className="flex items-end gap-2">
          <Select
            label="Status"
            value={status}
            onChange={event => setStatus(event.target.value as '' | NotificationDeliveryStatus)}
            wrapperClassName="w-[170px]"
          >
            {STATUS_OPTIONS.map(option => <option key={option.value || 'all'} value={option.value}>{option.label}</option>)}
          </Select>
          <Button size="sm" variant="secondary" icon={<RefreshCw size={13} />} onClick={() => load()} disabled={loading || !organizationId}>
            Refresh
          </Button>
        </div>
      </div>

      {loading ? <LoadingState label="Loading notification delivery history" /> : error ? (
        <ErrorState title="Delivery history is unavailable" description={error} onRetry={() => load()} />
      ) : deliveries.length === 0 ? (
        <EmptyState
          icon={<History size={24} />}
          title={status ? `No ${status} deliveries` : 'No delivery history'}
          description={status ? 'Try another status or refresh after an automation event runs.' : 'Delivery attempts will appear here after an attached automation event runs.'}
        />
      ) : (
        <>
          <div className="overflow-hidden rounded-xl border border-line bg-panel">
            <div className="grid grid-cols-[minmax(190px,1.3fr)_minmax(150px,1fr)_120px_150px] gap-4 border-b border-line px-4 py-2.5 font-mono text-[10px] uppercase tracking-[0.12em] text-dim max-[800px]:hidden">
              <span>Event</span><span>Target</span><span>Status</span><span>Updated</span>
            </div>
            {deliveries.map(delivery => <DeliveryRow key={delivery.id} delivery={delivery} />)}
          </div>
          {nextCursor && (
            <div className="mt-4 flex justify-center">
              <Button variant="secondary" disabled={loadingMore} onClick={() => load(nextCursor)}>
                {loadingMore ? 'Loading…' : 'Load older deliveries'}
              </Button>
            </div>
          )}
        </>
      )}
    </section>
  );
};

const DeliveryRow: React.FC<{ delivery: NotificationDelivery }> = ({ delivery }) => {
  const hasAttempts = delivery.attempts.length > 0;
  const statusIcon = delivery.status === 'delivered'
    ? <CheckCircle2 size={13} className="text-ok" />
    : delivery.status === 'failed'
      ? <AlertTriangle size={13} className="text-err" />
      : <Clock3 size={13} className="text-warn" />;
  return (
    <details className="group border-b border-line last:border-b-0">
      <summary className="grid cursor-pointer list-none grid-cols-[minmax(190px,1.3fr)_minmax(150px,1fr)_120px_150px] gap-4 px-4 py-3 hover:bg-raised/60 max-[800px]:grid-cols-1 max-[800px]:gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2 truncate text-[13px] font-medium text-ink">
            {statusIcon}<span className="truncate">{delivery.subject_name}</span>
          </div>
          <div className="mt-1 font-mono text-[10.5px] text-dim">
            {resourceLabel(delivery.resource_type)} #{delivery.resource_id} · {eventLabel(delivery.event)}
            {delivery.team_name ? ` · ${delivery.team_name}` : ''}
          </div>
        </div>
        <div className="min-w-0">
          <div className="truncate text-[12px] text-ink2">{delivery.target_name}</div>
          <div className="mt-1 font-mono text-[10.5px] text-dim">{delivery.target_type}</div>
        </div>
        <div>{statusBadge(delivery.status)}</div>
        <div className="font-mono text-[10.5px] text-dim">
          {new Date(delivery.updated_at).toLocaleString()}
          <div className="mt-1">{delivery.attempt_count}/{delivery.max_attempts} attempts</div>
        </div>
      </summary>
      <div className="border-t border-line bg-base/40 px-4 py-3">
        {delivery.failure_reason && (
          <div className="mb-3 rounded-lg border border-err/20 bg-err/5 px-3 py-2 text-xs text-ink2">
            <span className="font-mono text-[10px] uppercase tracking-wide text-err">{delivery.failure_code || 'delivery_failed'}</span>
            <p className="mt-1">{delivery.failure_reason}</p>
          </div>
        )}
        {!hasAttempts ? <p className="text-xs text-dim">No delivery attempt has started yet.</p> : (
          <ol aria-label={`Attempts for ${delivery.subject_name}`} className="space-y-2">
            {delivery.attempts.map(attempt => (
              <li key={attempt.attempt_number} className="flex flex-wrap items-start justify-between gap-2 text-xs">
                <div>
                  <span className="font-medium text-ink2">Attempt {attempt.attempt_number}</span>
                  <span className="ml-2 text-mut">{eventLabel(attempt.outcome)}</span>
                  {attempt.failure_reason && <p className="mt-0.5 text-dim">{attempt.failure_reason}</p>}
                </div>
                <time className="font-mono text-[10.5px] text-dim" dateTime={attempt.finished_at}>
                  {new Date(attempt.finished_at).toLocaleString()}
                </time>
              </li>
            ))}
          </ol>
        )}
      </div>
    </details>
  );
};

export default NotificationDeliveryHistory;
