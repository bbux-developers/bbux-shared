import type { FilterField } from '@bbux/types';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { CircleDot } from 'lucide-react';
import type { ReactElement } from 'react';
import { describe, expect, it } from 'vitest';
import { applyClientColors, datumColorForSliceValue, sliceOptionFor } from '../chart-view';
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from '../dropdown-menu';
import { FilterPopover } from '../filter-popover';

// Coloured filter options: a `FilterValue.color` token should
//   (a) render a coloured dot beside the option in the picker submenu,
//   (b) flow onto SliceOption.values (sliceOptionFor), and
//   (c) map to a recharts fill CSS var for the chart datum
//       (datumColorForSliceValue) — undefined when the value has no colour.

const STATUS_FIELD: FilterField = {
  kind: 'static',
  key: 'status',
  label: 'Status',
  icon: CircleDot,
  iconClassName: 'text-text-muted',
  values: [
    { value: 'Active', label: 'Active', color: 'success' },
    { value: 'Invited', label: 'Invited', color: 'warning' },
    { value: 'Plain', label: 'Plain' },
  ],
};

function renderPicker(): void {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const ui: ReactElement = (
    <QueryClientProvider client={qc}>
      <DropdownMenu defaultOpen>
        <DropdownMenuTrigger>filters</DropdownMenuTrigger>
        <DropdownMenuContent>
          <FilterPopover fields={[STATUS_FIELD]} active={{}} onChange={() => {}} />
        </DropdownMenuContent>
      </DropdownMenu>
    </QueryClientProvider>
  );
  render(ui);
}

describe('filter picker — coloured option dot', () => {
  it('renders a dot with the resolved colour class for a coloured value, and none for an uncoloured value', async () => {
    const user = userEvent.setup();
    renderPicker();

    // Open the Status submenu to reveal its value items.
    const trigger = await screen.findByTestId('popover-filter-status');
    await user.hover(trigger);

    // Coloured values render a bg-current dot tinted by the resolved class.
    const active = await screen.findByText('Active');
    const activeRow = active.closest('[role="menuitemcheckbox"]');
    expect(activeRow).not.toBeNull();
    expect(activeRow?.querySelector('.text-success')).not.toBeNull();

    const invited = await screen.findByText('Invited');
    const invitedRow = invited.closest('[role="menuitemcheckbox"]');
    expect(invitedRow?.querySelector('.text-warning')).not.toBeNull();

    // Uncoloured value renders no colour dot.
    const plain = await screen.findByText('Plain');
    const plainRow = plain.closest('[role="menuitemcheckbox"]');
    expect(plainRow).not.toBeNull();
    // No tinted dot span present.
    expect(plainRow?.querySelector('.rounded-full')).toBeNull();
  });
});

describe('sliceOptionFor — carries colour', () => {
  it('maps FilterValue.color onto SliceOption.values', () => {
    const opt = sliceOptionFor(STATUS_FIELD);
    expect(opt).not.toBeNull();
    expect(opt?.values).toEqual([
      { value: 'Active', label: 'Active', color: 'success' },
      { value: 'Invited', label: 'Invited', color: 'warning' },
      { value: 'Plain', label: 'Plain', color: undefined },
    ]);
  });
});

describe('datumColorForSliceValue — chart fill mapping', () => {
  it('maps a colour token to a recharts CSS-var fill', () => {
    expect(datumColorForSliceValue('success')).toBe('var(--success)');
    expect(datumColorForSliceValue('warning')).toBe('var(--warning)');
    expect(datumColorForSliceValue('danger')).toBe('var(--danger)');
  });

  it('leaves the fill undefined for an uncoloured value', () => {
    expect(datumColorForSliceValue(undefined)).toBeUndefined();
  });
});

describe('applyClientColors — CLIENT-mode per-column field colour', () => {
  const distinct = [
    { label: 'Active', count: 5 },
    { label: 'Invited', count: 2 },
    { label: 'Plain', count: 1 },
  ];

  it('colours EVERY bar with the matched static field icon colour (per-column, like bbux)', () => {
    const field = { ...STATUS_FIELD, iconClassName: 'text-success' };
    const out = applyClientColors(distinct, 'status', [field]);
    expect(out.every((d) => d.color === 'var(--success)')).toBe(true);
  });

  it('leaves every datum uncoloured when no field matches the column', () => {
    const out = applyClientColors(distinct, 'amount', [STATUS_FIELD]);
    expect(out.every((d) => d.color === undefined)).toBe(true);
  });

  it('leaves every datum uncoloured when no filterFields are provided', () => {
    const out = applyClientColors(distinct, 'status', undefined);
    expect(out.every((d) => d.color === undefined)).toBe(true);
  });
});
