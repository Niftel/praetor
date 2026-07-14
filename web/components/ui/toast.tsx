import React, { useEffect, useState } from 'react';
import { CheckCircle, XCircle, Info, X } from 'lucide-react';
import Modal from './Modal';
import Button from './Button';

// Imperative toast + confirm API (react-hot-toast style) backed by a small
// module-level emitter, so any module — not just components — can call
// `toast.error(...)` / `await confirmDialog(...)` without threading context.
// Render <ToastHost /> once, near the app root.

type ToastKind = 'success' | 'error' | 'info';
interface ToastItem { id: number; kind: ToastKind; message: string; }

let counter = 0;
const toastListeners = new Set<(t: ToastItem[]) => void>();
let toastItems: ToastItem[] = [];
const emitToasts = () => toastListeners.forEach((l) => l(toastItems));

function push(kind: ToastKind, message: string) {
  const id = ++counter;
  toastItems = [...toastItems, { id, kind, message }];
  emitToasts();
  const ttl = kind === 'error' ? 6000 : 4000;
  setTimeout(() => dismiss(id), ttl);
}
function dismiss(id: number) {
  toastItems = toastItems.filter((t) => t.id !== id);
  emitToasts();
}

export const toast = {
  success: (message: string) => push('success', message),
  error: (message: string) => push('error', message),
  info: (message: string) => push('info', message),
};

interface ConfirmReq {
  message: string;
  title: string;
  confirmText: string;
  destructive: boolean;
  resolve: (v: boolean) => void;
}
const confirmListeners = new Set<(c: ConfirmReq | null) => void>();
let currentConfirm: ConfirmReq | null = null;
const emitConfirm = () => confirmListeners.forEach((l) => l(currentConfirm));

// confirmDialog replaces window.confirm — returns a promise resolving to the
// user's choice. Destructive by default (red confirm button).
export function confirmDialog(
  message: string,
  opts?: { title?: string; confirmText?: string; destructive?: boolean },
): Promise<boolean> {
  return new Promise((resolve) => {
    currentConfirm = {
      message,
      title: opts?.title ?? 'Please confirm',
      confirmText: opts?.confirmText ?? 'Confirm',
      destructive: opts?.destructive ?? true,
      resolve,
    };
    emitConfirm();
  });
}

const kindStyles: Record<ToastKind, { ring: string; icon: React.ReactNode }> = {
  success: { ring: 'border-ok/30', icon: <CheckCircle className="text-ok" size={18} /> },
  error: { ring: 'border-err/30', icon: <XCircle className="text-err" size={18} /> },
  info: { ring: 'border-run/30', icon: <Info className="text-run" size={18} /> },
};

export const ToastHost: React.FC = () => {
  const [items, setItems] = useState<ToastItem[]>(toastItems);
  const [confirm, setConfirm] = useState<ConfirmReq | null>(currentConfirm);

  useEffect(() => {
    toastListeners.add(setItems);
    confirmListeners.add(setConfirm);
    return () => {
      toastListeners.delete(setItems);
      confirmListeners.delete(setConfirm);
    };
  }, []);

  const resolveConfirm = (v: boolean) => {
    confirm?.resolve(v);
    currentConfirm = null;
    emitConfirm();
  };

  return (
    <>
      {/* Toast stack */}
      <div className="fixed top-4 right-4 z-[100] flex flex-col gap-2 w-80 max-w-[calc(100vw-2rem)]" role="region" aria-label="Notifications">
        {items.map((t) => (
          <div
            key={t.id}
            role="status"
            className={`flex items-start gap-2 bg-panel border ${kindStyles[t.kind].ring} rounded-lg shadow-2xl p-3 text-sm`}
          >
            <span className="shrink-0 mt-0.5">{kindStyles[t.kind].icon}</span>
            <span className="flex-1 text-ink2 whitespace-pre-wrap break-words">{t.message}</span>
            <button
              type="button"
              onClick={() => dismiss(t.id)}
              aria-label="Dismiss notification"
              className="shrink-0 text-dim hover:text-ink"
            >
              <X size={16} />
            </button>
          </div>
        ))}
      </div>

      {/* Confirm dialog */}
      {confirm && (
        <Modal isOpen title={confirm.title} onClose={() => resolveConfirm(false)} size="md">
          <p className="text-sm text-ink2 whitespace-pre-wrap">{confirm.message}</p>
          <div className="mt-5 flex justify-end gap-3">
            <Button type="button" variant="secondary" onClick={() => resolveConfirm(false)}>Cancel</Button>
            <Button
              type="button"
              variant={confirm.destructive ? 'danger' : 'primary'}
              onClick={() => resolveConfirm(true)}
            >
              {confirm.confirmText}
            </Button>
          </div>
        </Modal>
      )}
    </>
  );
};
