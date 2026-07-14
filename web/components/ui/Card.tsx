import React from 'react';

interface CardProps {
  children: React.ReactNode;
  className?: string;
  title?: string;
  action?: React.ReactNode;
  /** Adds a lift/interaction affordance when the whole card is clickable. */
  hoverable?: boolean;
  /** Removes the default body padding (for edge-to-edge tables etc.). */
  bodyClassName?: string;
}

const Card: React.FC<CardProps> = ({
  children,
  className = '',
  title,
  action,
  hoverable = false,
  bodyClassName = 'p-6',
}) => {
  return (
    <div
      className={
        'bg-panel rounded-xl border border-line ' +
        (hoverable
          ? 'transition-[border-color,transform] duration-200 hover:-translate-y-0.5 hover:border-line2 '
          : '') +
        className
      }
    >
      {(title || action) && (
        <div className="px-6 py-4 border-b border-line flex justify-between items-center gap-4">
          {title && (
            <h3 className="text-sm font-semibold tracking-tight text-ink">{title}</h3>
          )}
          {action && <div className="shrink-0">{action}</div>}
        </div>
      )}
      <div className={bodyClassName}>{children}</div>
    </div>
  );
};

export default Card;
