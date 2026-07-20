import React, { useState } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { FormActions, FormErrorSummary, SecretField, useDirtyFormGuard } from './Form';

afterEach(cleanup);

describe('form framework', () => {
  it('focuses and announces the validation summary', () => {
    const { rerender } = render(<FormErrorSummary errors={[]} />);
    rerender(<FormErrorSummary errors={['Name is required.']} />);
    expect(screen.getByRole('alert')).toBe(document.activeElement);
    expect(screen.getByText('Name is required.')).toBeTruthy();
  });

  it('blocks duplicate submission while saving', () => {
    render(<form><FormActions onCancel={() => {}} submitting submitLabel="Save credential" /></form>);
    const submit = screen.getByRole('button', { name: /Save credential/ });
    expect((submit as HTMLButtonElement).disabled).toBe(true);
    expect(submit.getAttribute('aria-busy')).toBe('true');
  });

  it('renders secrets as write-only password fields', () => {
    render(<SecretField label="Password" value="" onChange={() => {}} />);
    const input = screen.getByLabelText('Password') as HTMLInputElement;
    expect(input.type).toBe('password');
    expect(input.value).toBe('');
    expect(input.autocomplete).toBe('new-password');
    expect(screen.getByText(/Write-only/)).toBeTruthy();
  });

  it('registers a browser-exit warning only while dirty', () => {
    const add = vi.spyOn(window, 'addEventListener');
    const remove = vi.spyOn(window, 'removeEventListener');
    function Harness() {
      const [dirty, setDirty] = useState(false);
      useDirtyFormGuard(dirty);
      return <button onClick={() => setDirty(value => !value)}>toggle</button>;
    }
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: 'toggle' }));
    expect(add).toHaveBeenCalledWith('beforeunload', expect.any(Function));
    fireEvent.click(screen.getByRole('button', { name: 'toggle' }));
    expect(remove).toHaveBeenCalledWith('beforeunload', expect.any(Function));
  });
});
