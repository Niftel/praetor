import React from 'react';

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost';
  size?: 'sm' | 'md' | 'lg';
  icon?: React.ReactNode;
}

const Button: React.FC<ButtonProps> = ({
  children,
  variant = 'primary',
  size = 'md',
  icon,
  className = '',
  ...props
}) => {
  // Shared: smooth transition + a subtle physical press (translate on :active).
  const baseStyles =
    'inline-flex items-center justify-center font-medium rounded-lg select-none ' +
    'transition-[background-color,box-shadow,transform,border-color] duration-150 ease-out ' +
    'active:translate-y-px focus-visible:outline-none focus-visible:ring-2 ' +
    'focus-visible:ring-acc/60 focus-visible:ring-offset-2 focus-visible:ring-offset-bg ' +
    'disabled:opacity-50 disabled:cursor-not-allowed disabled:active:translate-y-0';

  const variants = {
    primary:
      'bg-acc text-[#04211d] hover:bg-acc2 active:bg-acc2',
    secondary:
      'bg-panel text-ink2 border border-line2 hover:border-white/25 hover:text-ink',
    danger:
      'bg-err/90 text-white hover:bg-err active:bg-err',
    ghost:
      'bg-transparent text-mut hover:bg-white/5 hover:text-ink',
  };

  const sizes = {
    sm: 'px-3 py-1.5 text-sm gap-1.5',
    md: 'px-4 py-2 text-sm gap-2',
    lg: 'px-6 py-2.5 text-base gap-2',
  };

  return (
    <button
      className={`${baseStyles} ${variants[variant]} ${sizes[size]} ${className}`}
      {...props}
    >
      {icon && <span className="-ml-0.5 shrink-0">{icon}</span>}
      {children}
    </button>
  );
};

export default Button;
