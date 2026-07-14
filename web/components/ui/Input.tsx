import React, { useId } from 'react';

// Shared form primitives with built-in label association (auto id/htmlFor),
// consistent styling, and focus rings. Replaces the 3+ divergent hand-rolled
// input styles and the missing label associations across the app.

const controlBase =
  'block w-full rounded-lg border border-line2 bg-panel px-3 py-2 text-sm text-ink ' +
  'placeholder:text-dim transition-[border-color,box-shadow] duration-150 ' +
  'focus:border-acc/60 focus:ring-2 focus:ring-acc/25 focus:outline-none ' +
  'disabled:bg-panel2 disabled:text-dim disabled:cursor-not-allowed';

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
      <label htmlFor={id} className="block text-sm font-medium text-ink2 mb-1">
        {label}
        {required && <span className="text-err"> *</span>}
      </label>
    )}
    {children(id)}
    {hint && !error && <p className="mt-1 text-xs text-mut">{hint}</p>}
    {error && <p className="mt-1 text-xs text-err">{error}</p>}
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
          className={`${controlBase} ${error ? 'border-err' : ''} ${className}`}
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
          className={`${controlBase} ${error ? 'border-err' : ''} ${className}`}
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
          className={`${controlBase} bg-panel ${error ? 'border-err' : ''} ${className}`}
          aria-invalid={error ? true : undefined}
          {...rest}
        >
          {children}
        </select>
      )}
    </Field>
  );
};
