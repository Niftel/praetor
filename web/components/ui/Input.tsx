import React, { useId } from 'react';

// Shared form primitives with built-in label association (auto id/htmlFor),
// consistent styling, and focus rings. Replaces the 3+ divergent hand-rolled
// input styles and the missing label associations across the app.

const controlBase =
  'block w-full rounded-md border border-gray-300 shadow-sm p-2 text-sm ' +
  'focus:border-brand-500 focus:ring-1 focus:ring-brand-500 focus:outline-none ' +
  'disabled:bg-gray-50 disabled:text-gray-500';

interface FieldWrapProps {
  label?: string;
  hint?: string;
  error?: string;
  required?: boolean;
  id: string;
  wrapperClassName?: string;
  children: (id: string) => React.ReactNode;
}

// Field renders a label bound (htmlFor/id) to the control it wraps, plus
// optional hint/error text. wrapperClassName lets callers place the field in a
// flex/grid layout.
const Field: React.FC<FieldWrapProps> = ({ label, hint, error, required, id, wrapperClassName, children }) => (
  <div className={wrapperClassName}>
    {label && (
      <label htmlFor={id} className="block text-sm font-medium text-gray-700 mb-1">
        {label}
        {required && <span className="text-red-500"> *</span>}
      </label>
    )}
    {children(id)}
    {hint && !error && <p className="mt-1 text-xs text-gray-500">{hint}</p>}
    {error && <p className="mt-1 text-xs text-red-600">{error}</p>}
  </div>
);

type InputProps = React.InputHTMLAttributes<HTMLInputElement> & {
  label?: string;
  hint?: string;
  error?: string;
  wrapperClassName?: string;
};

export const Input: React.FC<InputProps> = ({ label, hint, error, className = '', id, wrapperClassName, ...rest }) => {
  const auto = useId();
  const fieldId = id ?? auto;
  return (
    <Field label={label} hint={hint} error={error} required={rest.required} id={fieldId} wrapperClassName={wrapperClassName}>
      {(fid) => (
        <input
          id={fid}
          className={`${controlBase} ${error ? 'border-red-400' : ''} ${className}`}
          aria-invalid={error ? true : undefined}
          {...rest}
        />
      )}
    </Field>
  );
};

type TextareaProps = React.TextareaHTMLAttributes<HTMLTextAreaElement> & {
  label?: string;
  hint?: string;
  error?: string;
  wrapperClassName?: string;
};

export const Textarea: React.FC<TextareaProps> = ({ label, hint, error, className = '', id, wrapperClassName, ...rest }) => {
  const auto = useId();
  const fieldId = id ?? auto;
  return (
    <Field label={label} hint={hint} error={error} required={rest.required} id={fieldId} wrapperClassName={wrapperClassName}>
      {(fid) => (
        <textarea
          id={fid}
          className={`${controlBase} ${error ? 'border-red-400' : ''} ${className}`}
          aria-invalid={error ? true : undefined}
          {...rest}
        />
      )}
    </Field>
  );
};

type SelectProps = React.SelectHTMLAttributes<HTMLSelectElement> & {
  label?: string;
  hint?: string;
  error?: string;
  wrapperClassName?: string;
};

export const Select: React.FC<SelectProps> = ({ label, hint, error, className = '', id, children, wrapperClassName, ...rest }) => {
  const auto = useId();
  const fieldId = id ?? auto;
  return (
    <Field label={label} hint={hint} error={error} required={rest.required} id={fieldId} wrapperClassName={wrapperClassName}>
      {(fid) => (
        <select
          id={fid}
          className={`${controlBase} bg-white ${error ? 'border-red-400' : ''} ${className}`}
          aria-invalid={error ? true : undefined}
          {...rest}
        >
          {children}
        </select>
      )}
    </Field>
  );
};
